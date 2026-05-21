// Copyright 2026 The antigravity-sdk-go Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package local

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/hook"
	pb "github.com/zchee/antigravity-sdk-go/internal/localharnesspb"
)

// readerLoop reads OutputEvents from the websocket and routes them until the
// connection closes or its context is cancelled. It closes stepCh on exit so a
// ranging ReceiveSteps terminates. It runs in its own goroutine.
func (c *LocalConnection) readerLoop() {
	defer close(c.stepCh)
	for {
		_, data, err := c.ws.Read(c.readerCtx)
		if err != nil {
			c.handleReadError(err)
			return
		}
		var ev pb.OutputEvent
		if err := protojson.Unmarshal(data, &ev); err != nil {
			slog.Error("local: unmarshal output event", "err", err)
			c.emit(stepOrErr{err: &agtypes.ConnectionError{Message: "reader loop: " + err.Error(), Err: err}})
			return
		}
		switch ev.WhichEvent() {
		case pb.OutputEvent_StepUpdate_case:
			c.handleStepUpdate(&ev)
		case pb.OutputEvent_TrajectoryStateUpdate_case:
			c.handleTrajectoryStateUpdate(ev.GetTrajectoryStateUpdate())
		case pb.OutputEvent_ToolCall_case:
			tc := ev.GetToolCall()
			c.runBackground(func() { c.handleToolCall(tc) })
		}
	}
}

// handleReadError translates a websocket read error into the right terminal
// signal: a clean/expected close during Disconnect is silent; an unexpected
// close surfaces the harness stderr.
func (c *LocalConnection) handleReadError(err error) {
	c.mu.Lock()
	disconnecting := c.disconnecting
	c.mu.Unlock()
	if disconnecting || errors.Is(err, context.Canceled) {
		slog.Info("local: websocket closed; normal shutdown")
		return
	}
	if websocket.CloseStatus(err) != -1 {
		tail := c.stderr.tail()
		if tail == "" {
			tail = "(no stderr output)"
		}
		msg := "harness process exited unexpectedly (ws close " +
			strconv.Itoa(int(websocket.CloseStatus(err))) + ").\nHarness stderr:\n" + tail
		slog.Error(msg)
		c.emit(stepOrErr{err: &agtypes.ConnectionError{Message: msg}})
		return
	}
	c.emit(stepOrErr{err: &agtypes.ConnectionError{Message: "reader loop: " + err.Error(), Err: err}})
}

// emit pushes an item to the step channel, dropping it if the reader context is
// already cancelled (Disconnect path) to avoid blocking on a full channel.
func (c *LocalConnection) emit(item stepOrErr) {
	select {
	case c.stepCh <- item:
	case <-c.readerCtx.Done():
	}
}

// handleStepUpdate routes a step_update event: it updates the tracker, parses
// and enqueues the step, records the cascade id, dispatches observe-only and
// post-tool hooks, and processes wait-state requests.
func (c *LocalConnection) handleStepUpdate(ev *pb.OutputEvent) {
	su := ev.GetStepUpdate()
	key := makeStepKeyFromUpdate(su)

	c.mu.Lock()
	tracker, ok := c.stepTrackers[key]
	if !ok {
		tracker = newStepTracker()
		c.stepTrackers[key] = tracker
	}
	tracker.updateState(su.GetState())
	c.mu.Unlock()

	var usage *agtypes.UsageMetadata
	if ev.HasUsageMetadata() {
		u := parseUsageMetadata(ev.GetUsageMetadata())
		usage = &u
	}
	step, err := stepFromUpdate(su, usage)
	if err != nil {
		slog.Error("local: parse step update", "err", err)
		return
	}
	c.emit(stepOrErr{step: step})

	c.mu.Lock()
	if su.GetCascadeId() != "" && su.GetCascadeId() == su.GetTrajectoryId() {
		c.cascadeID = su.GetCascadeId()
	}
	cascadeID := c.cascadeID
	c.mu.Unlock()

	// Observe-only compaction hook.
	if step.Type == agtypes.StepTypeCompaction && c.hookRunner != nil {
		s := step
		c.runBackground(func() {
			c.mu.Lock()
			turnCtx := c.turnContext()
			c.mu.Unlock()
			if err := c.hookRunner.DispatchCompaction(c.readerCtx, turnCtx, s); err != nil {
				slog.Error("compaction hook error", "err", err)
			}
		})
	}

	// Track subagent model responses for the post-tool-call result.
	trajID := su.GetTrajectoryId()
	if cascadeID != "" && trajID != "" && trajID != cascadeID &&
		step.Source == agtypes.StepSourceModel && step.Content != "" {
		c.mu.Lock()
		c.subagentResponses[trajID] = step.Content
		c.mu.Unlock()
	}

	c.dispatchPendingBuiltin(su, key, step)
	c.processWaitState(su, key, tracker)
}

// dispatchPendingBuiltin fires the post-tool-call or on-tool-error hook for a
// previously-approved builtin tool call when its step reaches a terminal state.
func (c *LocalConnection) dispatchPendingBuiltin(su *pb.StepUpdate, key stepKey, step agtypes.Step) {
	if c.hookRunner == nil {
		return
	}
	state := su.GetState()
	c.mu.Lock()
	pc, ok := c.pendingBuiltin[key]
	if ok && (state == pb.StepUpdate_STATE_DONE || state == pb.StepUpdate_STATE_ERROR) {
		delete(c.pendingBuiltin, key)
	}
	c.mu.Unlock()
	if !ok {
		return
	}
	switch state {
	case pb.StepUpdate_STATE_DONE:
		var result any = step.Content
		if extracted := extractToolResult(su); extracted != nil {
			result = extracted
		}
		tr := agtypes.ToolResult{Name: pc.toolCall.Name, ID: pc.toolCall.ID, Result: result}
		c.runBackground(func() {
			if err := c.hookRunner.DispatchPostToolCall(c.readerCtx, pc.opCtx, tr); err != nil {
				slog.Error("post-tool-call hook error", "err", err)
			}
		})
	case pb.StepUpdate_STATE_ERROR:
		msg := su.GetErrorMessage()
		if msg == "" {
			msg = step.Content
		}
		if msg == "" {
			msg = "Built-in tool failed"
		}
		toolErr := errors.New(msg)
		c.runBackground(func() {
			if _, _, err := c.hookRunner.DispatchOnToolError(c.readerCtx, pc.opCtx, toolErr); err != nil {
				slog.Error("on-tool-error hook error", "err", err)
			}
		})
	}
}

// processWaitState launches one handler per request type when a step is waiting
// for the user, debounced via the tracker against harness rebroadcasts.
func (c *LocalConnection) processWaitState(su *pb.StepUpdate, key stepKey, tracker *stepTracker) {
	if su.GetState() != pb.StepUpdate_STATE_WAITING_FOR_USER {
		return
	}
	if su.HasQuestionsRequest() {
		c.mu.Lock()
		fresh := tracker.markHandled("questions_request")
		c.mu.Unlock()
		if fresh {
			c.runBackground(func() { c.handleQuestionRequest(su) })
		}
	}
	if su.HasToolConfirmationRequest() {
		c.mu.Lock()
		fresh := tracker.markHandled("tool_confirmation_request")
		c.mu.Unlock()
		if fresh {
			c.runBackground(func() { c.handleToolConfirmationRequest(su) })
		}
	}
}

// handleTrajectoryStateUpdate tracks parent/subagent run/idle transitions and
// signals connection idle when the parent and all subagents have completed.
func (c *LocalConnection) handleTrajectoryStateUpdate(tsu *pb.TrajectoryStateUpdate) {
	c.mu.Lock()
	cascadeID := c.cascadeID
	trajID := tsu.GetTrajectoryId()
	isSubagent := cascadeID != "" && trajID != cascadeID

	switch tsu.GetState() {
	case pb.TrajectoryStateUpdate_STATE_RUNNING:
		if isSubagent {
			c.activeSubagents[trajID] = struct{}{}
		}
		c.mu.Unlock()
		return
	case pb.TrajectoryStateUpdate_STATE_IDLE:
		if isSubagent {
			delete(c.activeSubagents, trajID)
			response := c.subagentResponses[trajID]
			delete(c.subagentResponses, trajID)
			// Compute the turn context only when there is a hook runner to
			// dispatch through; turnContext panics on a nil runner because it
			// reads runner.SessionContext.
			if c.hookRunner != nil {
				turnCtx := c.turnContext()
				c.mu.Unlock()
				opCtx := hook.NewOperationContext(turnCtx)
				result := response
				if result == "" {
					result = trajID
				}
				tr := agtypes.ToolResult{Name: string(agtypes.ToolStartSubagent), Result: result}
				if err := c.hookRunner.DispatchPostToolCall(c.readerCtx, opCtx, tr); err != nil {
					slog.Error("subagent post-tool-call hook error", "err", err)
				}
				c.mu.Lock()
			}
		} else {
			c.parentIdle = true
		}
		idleNow := c.parentIdle && len(c.activeSubagents) == 0
		c.mu.Unlock()
		if idleNow {
			c.setIdle(true)
		}
		return
	default:
		c.mu.Unlock()
	}
}

// handleQuestionRequest dispatches the interaction hook for a questions_request
// and sends the answers back. The harness must always receive a response, so
// failures send an error answer rather than hanging.
func (c *LocalConnection) handleQuestionRequest(su *pb.StepUpdate) {
	answers, err := c.buildQuestionAnswers(su)
	if err != nil {
		slog.Error("local: handle question request", "err", err)
		errAns := pb.UserQuestionAnswer_builder{
			MultipleChoiceAnswer: pb.MultipleChoiceAnswer_builder{
				FreeformResponse: protoString("SDK error processing question: " + err.Error()),
			}.Build(),
		}.Build()
		answers = []*pb.UserQuestionAnswer{errAns}
	}
	c.sendQuestionResponse(su, answers)
}

// buildQuestionAnswers maps the harness's multiple-choice questions through the
// interaction hook into proto answers (unanswered when no hook handles them).
func (c *LocalConnection) buildQuestionAnswers(su *pb.StepUpdate) ([]*pb.UserQuestionAnswer, error) {
	reqQuestions := su.GetQuestionsRequest().GetQuestions()
	answers := make([]*pb.UserQuestionAnswer, len(reqQuestions))
	for i := range answers {
		answers[i] = pb.UserQuestionAnswer_builder{Unanswered: protoBool(true)}.Build()
	}

	var spec []agtypes.AskQuestionEntry
	var hookIndices []int
	for i, uq := range reqQuestions {
		if uq.HasMultipleChoice() {
			mc := uq.GetMultipleChoice()
			opts := make([]agtypes.AskQuestionOption, len(mc.GetChoices()))
			for j, choice := range mc.GetChoices() {
				opts[j] = agtypes.AskQuestionOption{ID: strconv.Itoa(j + 1), Text: choice}
			}
			spec = append(spec, agtypes.AskQuestionEntry{Question: mc.GetQuestion(), Options: opts})
			hookIndices = append(hookIndices, i)
		}
	}

	if c.hookRunner == nil || len(spec) == 0 {
		return answers, nil
	}

	c.mu.Lock()
	turnCtx := c.turnContext()
	c.mu.Unlock()
	_, qr, _, err := c.hookRunner.DispatchInteraction(c.readerCtx, turnCtx, agtypes.AskQuestionInteractionSpec{Questions: spec})
	if err != nil {
		return nil, err
	}
	for n, idx := range hookIndices {
		if n >= len(qr.Responses) {
			break
		}
		answers[idx] = questionAnswerProto(qr.Responses[n])
	}
	return answers, nil
}

// questionAnswerProto converts a hook QuestionResponse to the proto answer.
func questionAnswerProto(r agtypes.QuestionResponse) *pb.UserQuestionAnswer {
	if r.Skipped {
		return pb.UserQuestionAnswer_builder{Unanswered: protoBool(true)}.Build()
	}
	mc := &pb.MultipleChoiceAnswer_builder{}
	if len(r.SelectedOptionIDs) > 0 {
		indices := make([]int32, 0, len(r.SelectedOptionIDs))
		for _, id := range r.SelectedOptionIDs {
			if n, err := strconv.Atoi(id); err == nil {
				indices = append(indices, int32(n-1))
			}
		}
		mc.SelectedChoiceIndices = indices
	}
	if r.FreeformResponse != "" {
		mc.FreeformResponse = protoString(r.FreeformResponse)
	}
	return pb.UserQuestionAnswer_builder{MultipleChoiceAnswer: mc.Build()}.Build()
}

// sendQuestionResponse writes a UserQuestionsResponse for the given step.
func (c *LocalConnection) sendQuestionResponse(su *pb.StepUpdate, answers []*pb.UserQuestionAnswer) {
	resp := pb.UserQuestionsResponse_builder{
		TrajectoryId: protoString(su.GetTrajectoryId()),
		StepIndex:    protoUint32(su.GetStepIndex()),
		Response: pb.UserQuestionsResponse_QuestionsResponse_builder{
			Answers: answers,
		}.Build(),
	}.Build()
	if err := c.writeEvent(c.readerCtx, pb.InputEvent_builder{QuestionResponse: resp}.Build()); err != nil {
		slog.Error("local: send question response", "err", err)
	}
}

// handleToolConfirmationRequest runs the pre-tool-call decide hook (policy) for
// a builtin tool the harness wants to run and replies with accept/reject. A
// pre-request for a host tool is auto-approved (its real call follows).
func (c *LocalConnection) handleToolConfirmationRequest(su *pb.StepUpdate) {
	tc, allow := c.decideToolConfirmation(su)
	c.sendToolConfirmation(su, allow)
	_ = tc
}

// decideToolConfirmation derives the ToolCall, runs the policy hook, and tracks
// the approved call for its later post-tool-call hook. It returns the call and
// the allow decision.
func (c *LocalConnection) decideToolConfirmation(su *pb.StepUpdate) (agtypes.ToolCall, bool) {
	name, msg := activeAction(su)
	args := map[string]any{}
	if name != "" && msg != nil {
		if m, err := protoToMap(msg); err == nil {
			args = m
		}
	} else {
		name = defaultHostToolName
	}
	if rt := su.GetRequestText(); rt != "" {
		args["request_text"] = rt
	}
	canonical := normalizePathArgs(args)
	tc := agtypes.ToolCall{
		ID:            makeStepID(su.GetTrajectoryId(), su.GetStepIndex()),
		Name:          name,
		Args:          args,
		CanonicalPath: canonical,
	}

	if tc.Name == defaultHostToolName || c.hookRunner == nil {
		return tc, true
	}

	c.mu.Lock()
	turnCtx := c.turnContext()
	c.mu.Unlock()
	res, _, opCtx, err := c.hookRunner.DispatchPreToolCall(c.readerCtx, turnCtx, tc)
	if err != nil {
		slog.Error("pre-tool-call hook error; rejecting", "err", err)
		return tc, false
	}
	if res.Allow {
		key := stepKey{trajectoryID: su.GetTrajectoryId(), stepIndex: su.GetStepIndex()}
		c.mu.Lock()
		c.pendingBuiltin[key] = pendingCall{toolCall: tc, opCtx: opCtx}
		c.mu.Unlock()
	}
	return tc, res.Allow
}

// sendToolConfirmation writes the accept/reject decision for a step.
func (c *LocalConnection) sendToolConfirmation(su *pb.StepUpdate, accepted bool) {
	resp := pb.ToolConfirmation_builder{
		TrajectoryId: protoString(su.GetTrajectoryId()),
		StepIndex:    protoUint32(su.GetStepIndex()),
		Accepted:     protoBool(accepted),
	}.Build()
	if err := c.writeEvent(c.readerCtx, pb.InputEvent_builder{ToolConfirmation: resp}.Build()); err != nil {
		slog.Error("local: send tool confirmation", "err", err)
	}
}

// handleToolCall executes a host-side tool call requested by the harness:
// enqueue a tool-call step, run the pre-tool-call hook (deny → error result),
// execute via the tool runner, dispatch post/error hooks, and send the result.
func (c *LocalConnection) handleToolCall(tcProto *pb.ToolCall) {
	id := tcProto.GetId()
	name := tcProto.GetName()
	var args map[string]any
	if aj := tcProto.GetArgumentsJson(); aj != "" {
		if err := gojsonUnmarshal(aj, &args); err != nil {
			slog.Error("local: parse tool call args", "err", err)
		}
	}
	tc := agtypes.ToolCall{ID: id, Name: name, Args: args}

	c.emit(stepOrErr{step: agtypes.Step{
		ID:        id,
		StepIndex: 1,
		Type:      agtypes.StepTypeToolCall,
		Source:    agtypes.StepSourceModel,
		Target:    agtypes.StepTargetEnvironment,
		Status:    agtypes.StepStatusActive,
		ToolCalls: []agtypes.ToolCall{tc},
	}})

	var opCtx *hook.Context
	if c.hookRunner != nil {
		c.mu.Lock()
		turnCtx := c.turnContext()
		c.mu.Unlock()
		res, _, oc, err := c.hookRunner.DispatchPreToolCall(c.readerCtx, turnCtx, tc)
		if err != nil {
			slog.Error("pre-tool-call hook error", "err", err)
		}
		opCtx = oc
		if err == nil && !res.Allow {
			reason := res.Message
			if reason == "" {
				reason = "No reason provided"
			}
			_ = c.SendToolResults(c.readerCtx, []agtypes.ToolResult{{
				ID: id, Name: name, Error: "Tool execution denied by hook policy: " + reason,
			}})
			return
		}
	}

	if c.toolRunner == nil {
		slog.Warn("local: tool call but no tool runner configured", "tool", name)
		return
	}

	results := c.toolRunner.ProcessToolCalls(c.readerCtx, []agtypes.ToolCall{{Name: tc.Name, Args: tc.Args}})
	result := results[0]
	result.ID = id

	if result.Error != "" && c.hookRunner != nil {
		if opCtx == nil {
			c.mu.Lock()
			opCtx = hook.NewOperationContext(c.turnContext())
			c.mu.Unlock()
		}
		toolErr := result.Exception
		if toolErr == nil {
			toolErr = errors.New(result.Error)
		}
		recRes, recVal, herr := c.hookRunner.DispatchOnToolError(c.readerCtx, opCtx, toolErr)
		if herr == nil && recRes.Allow && recVal != nil {
			result = agtypes.ToolResult{ID: id, Name: name, Result: recVal}
		}
	} else if result.Error == "" && c.hookRunner != nil {
		if opCtx == nil {
			c.mu.Lock()
			opCtx = hook.NewOperationContext(c.turnContext())
			c.mu.Unlock()
		}
		if err := c.hookRunner.DispatchPostToolCall(c.readerCtx, opCtx, result); err != nil {
			slog.Error("post-tool-call hook error", "err", err)
		}
	}

	_ = c.SendToolResults(c.readerCtx, []agtypes.ToolResult{result})
}
