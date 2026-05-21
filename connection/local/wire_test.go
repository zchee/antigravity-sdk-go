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

// Wire-shape integration tests run in-process via the fake-WS harness. These
// exercise the reader loop and the policy/hook integration paths the
// per-method unit tests do not reach. They mirror a curated subset of the
// upstream connections/local/local_connection_test.py scenarios.

package local

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/hook"
	"github.com/zchee/antigravity-sdk-go/hook/policy"
	pb "github.com/zchee/antigravity-sdk-go/internal/localharnesspb"
	"github.com/zchee/antigravity-sdk-go/tool"
)

//  1. receive_steps_basic: text-response steps from the harness flow through
//     ReceiveSteps in order, and a parent-trajectory STATE_IDLE ends iteration.
func TestWireReceiveStepsBasic(t *testing.T) {
	h := newTestHarness(t, nil, nil)
	// Mark the connection busy first so ReceiveSteps actually iterates.
	h.conn.setIdle(false)
	h.conn.mu.Lock()
	h.conn.parentIdle = false
	h.conn.mu.Unlock()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	finish := h.drainSteps(ctx)

	for i, text := range []string{"one", "two", "three"} {
		h.sendStep(pb.StepUpdate_builder{
			TrajectoryId: proto.String("t1"),
			StepIndex:    proto.Uint32(uint32(i)),
			State:        pb.StepUpdate_STATE_DONE.Enum(),
			Source:       pb.StepUpdate_SOURCE_MODEL.Enum(),
			Target:       pb.StepUpdate_TARGET_USER.Enum(),
			Text:         proto.String(text),
			CascadeId:    proto.String("t1"),
		}.Build())
	}
	// Signal parent trajectory idle to end the stream.
	h.signalIdleTo("t1")

	steps, err := finish()
	if err != nil {
		t.Fatalf("ReceiveSteps error: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("got %d steps, want 3", len(steps))
	}
	for i, want := range []string{"one", "two", "three"} {
		s := steps[i].(agtypes.Step)
		if s.Content != want {
			t.Errorf("step[%d].Content = %q, want %q", i, s.Content, want)
		}
	}
}

//  2. tool_confirmation_request_integration: a tool_confirmation_request lands
//     the SDK in the wait state, policy approves, the SDK writes ToolConfirmation
//     {accepted:true} back, then a STATE_DONE on the same step triggers the
//     PostToolCall hook for the tracked pending builtin call.
func TestWireToolConfirmationIntegration(t *testing.T) {
	hr := hook.NewRunner()
	h, err := policy.Enforce([]policy.Policy{policy.AllowAll()})
	if err != nil {
		t.Fatal(err)
	}
	if err := hr.Register(h); err != nil {
		t.Fatal(err)
	}
	var postCalls atomic.Int32
	postDone := make(chan struct{}, 1)
	if err := hr.Register(hook.PostToolCall(func(_ context.Context, _ *hook.Context, _ agtypes.ToolResult) error {
		postCalls.Add(1)
		select {
		case postDone <- struct{}{}:
		default:
		}
		return nil
	})); err != nil {
		t.Fatal(err)
	}

	th := newTestHarness(t, nil, hr)
	// Request: harness is waiting on a run_command confirmation.
	th.sendStep(pb.StepUpdate_builder{
		TrajectoryId:            proto.String("t"),
		StepIndex:               proto.Uint32(7),
		State:                   pb.StepUpdate_STATE_WAITING_FOR_USER.Enum(),
		RunCommand:              pb.ActionRunCommand_builder{CommandLine: proto.String("ls")}.Build(),
		ToolConfirmationRequest: pb.ToolConfirmationRequest_builder{}.Build(),
	}.Build())
	// SDK writes ToolConfirmation back.
	resp := th.waitForResponse(time.Second)
	if !resp.HasToolConfirmation() {
		t.Fatalf("response missing tool_confirmation; got %+v", resp)
	}
	if !resp.GetToolConfirmation().GetAccepted() {
		t.Error("AllowAll policy did not accept the tool confirmation")
	}
	// Pending builtin call must be tracked for the post-tool-call hook.
	th.conn.mu.Lock()
	_, tracked := th.conn.pendingBuiltin[stepKey{trajectoryID: "t", stepIndex: 7}]
	th.conn.mu.Unlock()
	if !tracked {
		t.Fatal("approved tool confirmation was not tracked in pendingBuiltin")
	}

	// Harness reports DONE -> PostToolCall hook fires.
	th.sendStep(pb.StepUpdate_builder{
		TrajectoryId: proto.String("t"),
		StepIndex:    proto.Uint32(7),
		State:        pb.StepUpdate_STATE_DONE.Enum(),
		RunCommand:   pb.ActionRunCommand_builder{CombinedOutput: proto.String("hi\n")}.Build(),
	}.Build())
	select {
	case <-postDone:
	case <-time.After(time.Second):
		t.Fatal("PostToolCall hook did not fire on STATE_DONE")
	}
	if got := postCalls.Load(); got != 1 {
		t.Errorf("PostToolCall fired %d times, want 1", got)
	}
}

//  3. question_hook_integration: the interaction hook answers, SDK writes a
//     UserQuestionsResponse whose multiple_choice_answer carries the right
//     zero-based index.
func TestWireQuestionHookIntegration(t *testing.T) {
	hr := hook.NewRunner()
	if err := hr.Register(hook.OnInteraction(func(_ context.Context, _ *hook.Context, _ agtypes.AskQuestionInteractionSpec) (agtypes.QuestionHookResult, bool, error) {
		return agtypes.QuestionHookResult{
			Responses: []agtypes.QuestionResponse{{SelectedOptionIDs: []string{"2"}}},
		}, true, nil
	})); err != nil {
		t.Fatal(err)
	}
	th := newTestHarness(t, nil, hr)
	th.sendStep(pb.StepUpdate_builder{
		TrajectoryId: proto.String("t"),
		StepIndex:    proto.Uint32(3),
		State:        pb.StepUpdate_STATE_WAITING_FOR_USER.Enum(),
		QuestionsRequest: pb.UserQuestionsRequest_builder{
			Questions: []*pb.UserQuestion{
				pb.UserQuestion_builder{
					MultipleChoice: pb.MultipleChoice_builder{
						Question: proto.String("Choose"),
						Choices:  []string{"alpha", "beta", "gamma"},
					}.Build(),
				}.Build(),
			},
		}.Build(),
	}.Build())
	resp := th.waitForResponse(time.Second)
	if !resp.HasQuestionResponse() {
		t.Fatalf("response missing question_response; got %+v", resp)
	}
	answers := resp.GetQuestionResponse().GetResponse().GetAnswers()
	if len(answers) != 1 {
		t.Fatalf("got %d answers, want 1", len(answers))
	}
	idx := answers[0].GetMultipleChoiceAnswer().GetSelectedChoiceIndices()
	if len(idx) != 1 || idx[0] != 1 {
		t.Errorf("selected indices = %v, want [1]", idx)
	}
}

//  4. deduplication_of_wait_requests: the same questions_request sent twice
//     while in WAITING_FOR_USER fires the interaction hook only once.
func TestWireDeduplicatesWaitRequests(t *testing.T) {
	hr := hook.NewRunner()
	var fired atomic.Int32
	done := make(chan struct{}, 1)
	if err := hr.Register(hook.OnInteraction(func(_ context.Context, _ *hook.Context, _ agtypes.AskQuestionInteractionSpec) (agtypes.QuestionHookResult, bool, error) {
		fired.Add(1)
		select {
		case done <- struct{}{}:
		default:
		}
		return agtypes.QuestionHookResult{Cancelled: true}, true, nil
	})); err != nil {
		t.Fatal(err)
	}
	th := newTestHarness(t, nil, hr)
	q := pb.StepUpdate_builder{
		TrajectoryId: proto.String("t"),
		StepIndex:    proto.Uint32(1),
		State:        pb.StepUpdate_STATE_WAITING_FOR_USER.Enum(),
		QuestionsRequest: pb.UserQuestionsRequest_builder{
			Questions: []*pb.UserQuestion{pb.UserQuestion_builder{
				MultipleChoice: pb.MultipleChoice_builder{Question: proto.String("q"), Choices: []string{"a"}}.Build(),
			}.Build()},
		}.Build(),
	}.Build()
	th.sendStep(q)
	th.sendStep(q)
	// First fire happens; wait for it.
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("first interaction hook did not fire")
	}
	// Drain the first response and assert no second arrives.
	_ = th.waitForResponse(time.Second)
	th.expectNoResponse(200 * time.Millisecond)
	if got := fired.Load(); got != 1 {
		t.Errorf("interaction hook fired %d times, want 1 (debounced)", got)
	}
}

//  5. state_transition_clears_handled_requests: WAITING -> ACTIVE -> WAITING
//     causes the dedup set to clear, so the hook fires again on re-entry.
func TestWireStateTransitionClearsHandled(t *testing.T) {
	hr := hook.NewRunner()
	var fired atomic.Int32
	if err := hr.Register(hook.OnInteraction(func(_ context.Context, _ *hook.Context, _ agtypes.AskQuestionInteractionSpec) (agtypes.QuestionHookResult, bool, error) {
		fired.Add(1)
		return agtypes.QuestionHookResult{Cancelled: true}, true, nil
	})); err != nil {
		t.Fatal(err)
	}
	th := newTestHarness(t, nil, hr)
	makeQ := func(state pb.StepUpdate_State) *pb.StepUpdate {
		return pb.StepUpdate_builder{
			TrajectoryId: proto.String("t"),
			StepIndex:    proto.Uint32(1),
			State:        state.Enum(),
			QuestionsRequest: pb.UserQuestionsRequest_builder{
				Questions: []*pb.UserQuestion{pb.UserQuestion_builder{
					MultipleChoice: pb.MultipleChoice_builder{Question: proto.String("q"), Choices: []string{"a"}}.Build(),
				}.Build()},
			}.Build(),
		}.Build()
	}
	th.sendStep(makeQ(pb.StepUpdate_STATE_WAITING_FOR_USER))
	_ = th.waitForResponse(time.Second)
	// Leave waiting state, come back; the dedup set must have cleared.
	th.sendStep(pb.StepUpdate_builder{TrajectoryId: proto.String("t"), StepIndex: proto.Uint32(1), State: pb.StepUpdate_STATE_ACTIVE.Enum()}.Build())
	// Give the reader a moment to process the ACTIVE transition.
	waitFor(t, time.Second, func() bool {
		th.conn.mu.Lock()
		defer th.conn.mu.Unlock()
		tr, ok := th.conn.stepTrackers[stepKey{"t", 1}]
		return ok && tr.state == pb.StepUpdate_STATE_ACTIVE
	})
	th.sendStep(makeQ(pb.StepUpdate_STATE_WAITING_FOR_USER))
	_ = th.waitForResponse(time.Second)
	if got := fired.Load(); got != 2 {
		t.Errorf("interaction hook fired %d times, want 2 (re-entry)", got)
	}
}

//  6. subagent tracking: a subagent RUNNING keeps the connection busy even
//     after the parent goes IDLE, then a subagent IDLE flips the connection
//     idle.
func TestWireSubagentDelaysIdle(t *testing.T) {
	th := newTestHarness(t, nil, nil)
	th.conn.setIdle(false)
	th.conn.mu.Lock()
	th.conn.parentIdle = false
	th.conn.mu.Unlock()

	// Plant a parent step so cascadeID is established.
	th.sendStep(pb.StepUpdate_builder{
		TrajectoryId: proto.String("parent"),
		StepIndex:    proto.Uint32(0),
		State:        pb.StepUpdate_STATE_ACTIVE.Enum(),
		Source:       pb.StepUpdate_SOURCE_MODEL.Enum(),
		CascadeId:    proto.String("parent"),
	}.Build())
	waitFor(t, time.Second, func() bool {
		th.conn.mu.Lock()
		defer th.conn.mu.Unlock()
		return th.conn.cascadeID == "parent"
	})

	// Subagent starts.
	th.sendTrajectoryState(pb.TrajectoryStateUpdate_builder{
		TrajectoryId: proto.String("sub-1"),
		State:        pb.TrajectoryStateUpdate_STATE_RUNNING.Enum(),
	}.Build())
	// Parent goes idle, but the subagent is still running — connection MUST
	// remain busy.
	th.signalIdleTo("parent")
	// Give the reader a moment.
	time.Sleep(50 * time.Millisecond)
	if th.conn.IsIdle() {
		t.Fatal("connection went idle while subagent still running")
	}
	// Subagent goes idle; now the connection flips idle.
	th.sendTrajectoryState(pb.TrajectoryStateUpdate_builder{
		TrajectoryId: proto.String("sub-1"),
		State:        pb.TrajectoryStateUpdate_STATE_IDLE.Enum(),
	}.Build())
	waitFor(t, time.Second, th.conn.IsIdle)
}

//  7. handle_tool_call_queues_step + post_tool_call_hook_dispatched: a
//     host-side tool_call event runs through the tool runner, the SDK writes
//     a ToolResponse back with the result, and the PostToolCall hook fires.
func TestWireHostToolCallExecutes(t *testing.T) {
	tr := tool.NewRunner()
	if err := tr.AddTool(tool.ToolWithSchema{
		Name: "echo",
		Fn: func(_ context.Context, args map[string]any) (any, error) {
			return args["v"], nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	hr := hook.NewRunner()
	postCalls := make(chan agtypes.ToolResult, 1)
	if err := hr.Register(hook.PostToolCall(func(_ context.Context, _ *hook.Context, r agtypes.ToolResult) error {
		postCalls <- r
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	th := newTestHarness(t, tr, hr)

	th.sendToolCall("call-1", "echo", `{"v":"hi"}`)
	resp := th.waitForResponse(time.Second)
	if !resp.HasToolResponse() {
		t.Fatalf("expected tool_response, got %+v", resp)
	}
	if got := resp.GetToolResponse().GetId(); got != "call-1" {
		t.Errorf("ToolResponse.id = %q, want call-1", got)
	}
	rj := resp.GetToolResponse().GetResponseJson()
	if !strings.Contains(rj, "hi") {
		t.Errorf("ToolResponse.response_json = %q, want a result containing \"hi\"", rj)
	}
	select {
	case r := <-postCalls:
		if r.ID != "call-1" {
			t.Errorf("PostToolCall result id = %q, want call-1", r.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("PostToolCall hook did not fire for host tool")
	}
}

//  8. file:// URI arguments are normalized into ToolCall.CanonicalPath via the
//     step pipeline (the field the workspace policy checks).
func TestWireFileURINormalization(t *testing.T) {
	th := newTestHarness(t, nil, nil)
	th.conn.setIdle(false)
	th.conn.mu.Lock()
	th.conn.parentIdle = false
	th.conn.mu.Unlock()

	finish := th.drainSteps(t.Context())
	th.sendStep(pb.StepUpdate_builder{
		TrajectoryId: proto.String("t"),
		StepIndex:    proto.Uint32(0),
		State:        pb.StepUpdate_STATE_ACTIVE.Enum(),
		ViewFile: pb.ActionViewFile_builder{
			FilePath: proto.String("file:///ws/a%20file.txt"),
		}.Build(),
		CascadeId: proto.String("t"),
	}.Build())
	th.signalIdleTo("t")

	steps, err := finish()
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) == 0 {
		t.Fatal("no steps received")
	}
	s := steps[0].(agtypes.Step)
	if len(s.ToolCalls) != 1 {
		t.Fatalf("step.ToolCalls = %d, want 1", len(s.ToolCalls))
	}
	if got := s.ToolCalls[0].CanonicalPath; got != "/ws/a file.txt" {
		t.Errorf("CanonicalPath = %q, want %q", got, "/ws/a file.txt")
	}
}

// waitFor blocks until cond returns true or timeout elapses; fails the test
// on timeout. Used to await reader-loop side effects.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
