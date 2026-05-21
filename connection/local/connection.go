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
	"fmt"
	"io"
	"iter"
	"log/slog"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	gojson "github.com/go-json-experiment/json"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/connection"
	"github.com/zchee/antigravity-sdk-go/hook"
	pb "github.com/zchee/antigravity-sdk-go/internal/localharnesspb"
	"github.com/zchee/antigravity-sdk-go/tool"
)

// defaultHostToolName is the action name used for a tool-confirmation request
// that does not match a known builtin tool: a pre-request notification for a
// host-side tool whose specific call follows. Such pre-requests are
// auto-approved.
const defaultHostToolName = "pre_request_host_tool_request"

// stepKey identifies a trajectory step for tracking state and pending calls.
type stepKey struct {
	trajectoryID string
	stepIndex    uint32
}

// pendingCall records a builtin tool call approved via ToolConfirmation, so the
// post-tool-call (or on-tool-error) hook can fire when the step completes.
type pendingCall struct {
	toolCall agtypes.ToolCall
	opCtx    *hook.Context
}

// stepOrErr is one element of the internal step stream: a step, or a terminal
// error that ends iteration.
type stepOrErr struct {
	step agtypes.Step
	err  error
}

// LocalConnection is a Connection to the Go localharness binary over a
// websocket. It embeds connection.BaseConnection for the lifecycle no-ops it
// does not override (SignalIdle, WaitForWakeup, Delete).
//
// Writes to the websocket are serialized by writeMu; the single reader loop
// runs in its own goroutine, feeding a buffered step channel that ReceiveSteps
// ranges over. Hook dispatches that may block (interaction, confirmation) run
// in their own goroutines tracked by bgWG.
// wsConn is the narrow websocket surface LocalConnection depends on. The real
// *websocket.Conn satisfies it structurally (see the compile-time assertion
// below); tests inject a fake to exercise the reader loop in-process.
type wsConn interface {
	Read(ctx context.Context) (websocket.MessageType, []byte, error)
	Write(ctx context.Context, typ websocket.MessageType, data []byte) error
	Close(code websocket.StatusCode, reason string) error
}

var _ wsConn = (*websocket.Conn)(nil)

type LocalConnection struct {
	connection.BaseConnection

	ws         wsConn
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	toolRunner *tool.Runner
	hookRunner *hook.Runner

	writeMu sync.Mutex // serializes ws.Write calls

	readerCtx    context.Context
	readerCancel context.CancelFunc
	stepCh       chan stepOrErr
	bgWG         sync.WaitGroup // tracks background hook-dispatch goroutines

	mu                sync.Mutex // guards the fields below
	stepTrackers      map[stepKey]*stepTracker
	pendingBuiltin    map[stepKey]pendingCall
	cascadeID         string
	parentIdle        bool
	activeSubagents   map[string]struct{}
	subagentResponses map[string]string
	currentTurnCtx    *hook.Context
	cancelled         bool
	cancelledMessage  string
	disconnecting     bool
	receiving         bool

	idleMu sync.Mutex
	idle   bool
	// idleCh is closed when the connection becomes idle and replaced with a
	// fresh open channel when it goes busy; WaitForIdle selects on it. This
	// avoids the goroutine leak a sync.Cond would incur on ctx cancellation.
	idleCh chan struct{}

	stderr *stderrBuffer
}

// newLocalConnection wires a connection around an established websocket and the
// spawned process, and starts the reader loop. stderr captures the harness's
// recent stderr for diagnostics.
func newLocalConnection(ws wsConn, cmd *exec.Cmd, stdin io.WriteCloser, tr *tool.Runner, hr *hook.Runner, stderr *stderrBuffer) *LocalConnection {
	ctx, cancel := context.WithCancel(context.Background())
	c := &LocalConnection{
		ws:                ws,
		cmd:               cmd,
		stdin:             stdin,
		toolRunner:        tr,
		hookRunner:        hr,
		readerCtx:         ctx,
		readerCancel:      cancel,
		stepCh:            make(chan stepOrErr, 64),
		stepTrackers:      make(map[stepKey]*stepTracker),
		pendingBuiltin:    make(map[stepKey]pendingCall),
		activeSubagents:   make(map[string]struct{}),
		subagentResponses: make(map[string]string),
		parentIdle:        true,
		idle:              true,
		idleCh:            closedChan(),
		stderr:            stderr,
	}
	go c.readerLoop()
	return c
}

// closedChan returns an already-closed channel, representing the initial idle
// state.
func closedChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// IsIdle reports whether the connection is idle and ready for input.
func (c *LocalConnection) IsIdle() bool {
	c.idleMu.Lock()
	defer c.idleMu.Unlock()
	return c.idle
}

// ConversationID returns the cascade (conversation) id, or "" if not yet known.
func (c *LocalConnection) ConversationID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cascadeID
}

// setIdle records the idle state, closing idleCh to release WaitForIdle waiters
// on the busy→idle edge and replacing it with a fresh open channel on the
// idle→busy edge.
func (c *LocalConnection) setIdle(v bool) {
	c.idleMu.Lock()
	defer c.idleMu.Unlock()
	switch {
	case v && !c.idle:
		close(c.idleCh)
	case !v && c.idle:
		c.idleCh = make(chan struct{})
	}
	c.idle = v
}

// writeEvent serializes m to protojson and writes it as a text websocket
// message, serialized against concurrent writers.
func (c *LocalConnection) writeEvent(ctx context.Context, m proto.Message) error {
	data, err := protojson.Marshal(m)
	if err != nil {
		return fmt.Errorf("local: marshal event: %w", err)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.ws.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("local: write event: %w", err)
	}
	return nil
}

// Send sends a prompt to the agent. It dispatches the pre-turn hook (a denial
// cancels the turn) and forwards the prompt as a UserInput or ComplexUserInput
// event.
func (c *LocalConnection) Send(ctx context.Context, prompt agtypes.Content) error {
	c.mu.Lock()
	c.cancelled = false
	c.cancelledMessage = ""
	c.parentIdle = false
	clear(c.activeSubagents)
	clear(c.subagentResponses)
	c.mu.Unlock()
	c.setIdle(false)

	if c.hookRunner != nil {
		res, turnCtx, err := c.hookRunner.DispatchPreTurn(ctx, prompt)
		if err != nil {
			return err
		}
		c.mu.Lock()
		c.currentTurnCtx = turnCtx
		c.mu.Unlock()
		if !res.Allow {
			slog.Warn("turn denied by hook", "message", res.Message)
			msg := res.Message
			if msg == "" {
				msg = "Turn execution denied by hook."
			}
			c.mu.Lock()
			c.cancelled = true
			c.cancelledMessage = msg
			c.mu.Unlock()
			c.setIdle(true)
			return nil
		}
	}

	event, err := promptToInputEvent(prompt)
	if err != nil {
		return err
	}
	return c.writeEvent(ctx, event)
}

// promptToInputEvent builds the InputEvent for a prompt: an empty/absent prompt
// and a string become user_input; a Media or slice becomes complex_user_input.
func promptToInputEvent(prompt agtypes.Content) (*pb.InputEvent, error) {
	switch p := prompt.(type) {
	case nil:
		return pb.InputEvent_builder{UserInput: proto.String("")}.Build(), nil
	case string:
		return pb.InputEvent_builder{UserInput: proto.String(p)}.Build(), nil
	}
	var primitives []agtypes.ContentPrimitive
	if list, ok := prompt.([]agtypes.ContentPrimitive); ok {
		primitives = list
	} else {
		primitives = []agtypes.ContentPrimitive{prompt}
	}
	parts := make([]*pb.UserInput_Part, 0, len(primitives))
	for _, c := range primitives {
		part, err := toProtoInputContent(c)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	ui := pb.UserInput_builder{Parts: parts}.Build()
	return pb.InputEvent_builder{ComplexUserInput: ui}.Build(), nil
}

// ReceiveSteps returns an iterator over steps recorded as the agent produces
// them. Only one iteration may run at a time; a concurrent call yields
// ErrConcurrentReceive. A terminal stream error is yielded as a final
// (zero, err). The iterator returns when the turn goes idle.
func (c *LocalConnection) ReceiveSteps(ctx context.Context) iter.Seq2[agtypes.Step, error] {
	return func(yield func(agtypes.Step, error) bool) {
		c.mu.Lock()
		if c.receiving {
			c.mu.Unlock()
			yield(agtypes.Step{}, ErrConcurrentReceive)
			return
		}
		c.receiving = true
		cancelled, cancelledMsg := c.cancelled, c.cancelledMessage
		c.mu.Unlock()
		defer func() {
			c.mu.Lock()
			c.receiving = false
			c.mu.Unlock()
		}()

		if cancelled {
			yield(agtypes.Step{
				Status: agtypes.StepStatusCanceled,
				Error:  cancelledMsg,
				Source: agtypes.StepSourceSystem,
				Type:   agtypes.StepTypeSystemMessage,
			}, nil)
			return
		}

		if c.IsIdle() && len(c.stepCh) == 0 {
			return
		}

		for {
			if c.IsIdle() && len(c.stepCh) == 0 {
				return
			}
			// Snapshot idleCh under its mutex; setIdle(true) closes the channel
			// we capture here so a select waking on it lets us re-check
			// IsIdle+empty and terminate. Without this case, the loop would
			// block on stepCh after the last step was yielded.
			c.idleMu.Lock()
			idleCh := c.idleCh
			c.idleMu.Unlock()
			select {
			case <-ctx.Done():
				yield(agtypes.Step{}, ctx.Err())
				return
			case <-idleCh:
				// Loop back to the top idle+empty check.
				continue
			case item, ok := <-c.stepCh:
				if !ok {
					return
				}
				if item.err != nil {
					yield(agtypes.Step{}, item.err)
					return
				}
				step := item.step
				if !yield(step, nil) {
					return
				}
				if fatal := c.checkSystemError(step); fatal != nil {
					yield(agtypes.Step{}, fatal)
					return
				}
				c.maybeDispatchPostTurn(ctx, step)
			}
		}
	}
}

// checkSystemError returns a fatal error for known-fatal system error steps
// (HTTP 400/401/403); other system errors are logged and tolerated.
func (c *LocalConnection) checkSystemError(step agtypes.Step) error {
	if step.Status != agtypes.StepStatusError || step.Source != agtypes.StepSourceSystem {
		return nil
	}
	code, _ := step.Extra[ExtraHTTPCode].(int)
	switch code {
	case 400, 401, 403:
		msg := step.Error
		if msg == "" {
			msg = "System error occurred."
		}
		return &agtypes.ConnectionError{Message: msg}
	default:
		slog.Warn("system step error", "http_code", code, "error", step.Error)
		return nil
	}
}

// maybeDispatchPostTurn fires the post-turn hook when a terminal model step
// directed at the user completes the turn.
func (c *LocalConnection) maybeDispatchPostTurn(ctx context.Context, step agtypes.Step) {
	isModel := step.Source == agtypes.StepSourceModel
	terminal := step.Status == agtypes.StepStatusDone ||
		step.Status == agtypes.StepStatusError ||
		step.Status == agtypes.StepStatusCanceled
	targetUser := step.Extra[ExtraWireTarget] == "TARGET_USER"
	if !(terminal && targetUser && isModel) {
		return
	}
	c.mu.Lock()
	turnCtx := c.currentTurnCtx
	c.currentTurnCtx = nil
	c.mu.Unlock()
	if c.hookRunner != nil && turnCtx != nil {
		if err := c.hookRunner.DispatchPostTurn(ctx, turnCtx, step.Content); err != nil {
			slog.Error("post-turn hook error", "err", err)
		}
	}
}

// WaitForIdle blocks until the connection becomes idle or ctx is cancelled.
func (c *LocalConnection) WaitForIdle(ctx context.Context) error {
	c.idleMu.Lock()
	if c.idle {
		c.idleMu.Unlock()
		return nil
	}
	ch := c.idleCh
	c.idleMu.Unlock()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Cancel requests the harness halt the current turn.
func (c *LocalConnection) Cancel(ctx context.Context) error {
	return c.writeEvent(ctx, pb.InputEvent_builder{HaltRequest: proto.Bool(true)}.Build())
}

// SendTriggerNotification sends a trigger message to the agent.
func (c *LocalConnection) SendTriggerNotification(ctx context.Context, content string) error {
	return c.writeEvent(ctx, pb.InputEvent_builder{AutomatedTrigger: proto.String(content)}.Build())
}

// SendToolResults sends host-tool execution results back to the harness. Each
// result must carry the id correlating it with the originating call.
func (c *LocalConnection) SendToolResults(ctx context.Context, results []agtypes.ToolResult) error {
	for _, r := range results {
		if r.ID == "" {
			return fmt.Errorf("local: tool result for %q is missing an id", r.Name)
		}
		respJSON, err := toolResultJSON(r)
		if err != nil {
			return err
		}
		resp := pb.ToolResponse_builder{
			Id:           proto.String(r.ID),
			ResponseJson: proto.String(respJSON),
		}.Build()
		if err := c.writeEvent(ctx, pb.InputEvent_builder{ToolResponse: resp}.Build()); err != nil {
			return err
		}
	}
	return nil
}

// toolResultJSON renders a tool result as the JSON object the harness expects:
// {"error": msg} on failure, the result marshaled as an object when it is one,
// or {"result": value} otherwise.
func toolResultJSON(r agtypes.ToolResult) (string, error) {
	var payload any
	switch {
	case r.Error != "":
		payload = map[string]any{"error": r.Error}
	default:
		// If the result already marshals to a JSON object, send it as-is;
		// otherwise wrap it under "result".
		b, err := gojson.Marshal(r.Result)
		if err != nil {
			payload = map[string]any{"result": fmt.Sprint(r.Result)}
			break
		}
		var obj map[string]any
		if err := gojson.Unmarshal(b, &obj); err == nil {
			payload = obj
		} else {
			payload = map[string]any{"result": r.Result}
		}
	}
	out, err := gojson.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("local: marshal tool result: %w", err)
	}
	return string(out), nil
}

// Disconnect tears down the connection in order: session-end hook, stop
// background dispatches and the reader, close the websocket, close stdin so the
// harness exits, then wait for the process (escalating to kill).
func (c *LocalConnection) Disconnect(ctx context.Context) error {
	c.mu.Lock()
	c.disconnecting = true
	c.mu.Unlock()

	var hookErr error
	if c.hookRunner != nil {
		if err := c.hookRunner.DispatchSessionEnd(ctx); err != nil {
			hookErr = err
		}
	}

	// Cancel the reader and wait for in-flight background hook dispatches.
	// Those dispatches run on readerCtx, so they observe cancellation here;
	// bgWG.Wait still blocks until each returns. This matches the upstream,
	// which cancels background tasks during disconnect.
	c.readerCancel()
	c.bgWG.Wait()

	// Close the websocket first: this triggers the harness's deferred cleanup
	// (agent close + trajectory serialization).
	_ = c.ws.Close(websocket.StatusNormalClosure, "")

	// Close stdin so the harness main loop sees EOF and exits cleanly, saving
	// its trajectory. Wait for it to exit on its own, bounded by the smaller of
	// disconnectGrace and the caller's context, then escalate to kill.
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		done := make(chan struct{})
		go func() { _ = c.cmd.Wait(); close(done) }()

		graceCtx, cancel := context.WithTimeout(ctx, disconnectGrace)
		defer cancel()
		select {
		case <-done:
		case <-graceCtx.Done():
			_ = c.cmd.Process.Kill()
			// Bound the post-kill wait so a wedged Wait cannot hang Disconnect.
			select {
			case <-done:
			case <-time.After(postKillGrace):
				slog.Error("local: harness process did not exit after kill")
			}
		}
	}
	return hookErr
}

// disconnectGrace is how long Disconnect waits for the harness to exit on its
// own after stdin is closed before escalating to kill (bounded further by the
// caller's context). postKillGrace bounds the wait after kill.
const (
	disconnectGrace = 5 * time.Second
	postKillGrace   = 2 * time.Second
)

// runBackground runs fn in a tracked goroutine so Disconnect can wait for
// in-flight hook dispatches.
func (c *LocalConnection) runBackground(fn func()) {
	c.bgWG.Go(fn)
}

// turnContext returns the current turn context, or a fresh one parented to the
// session context. The caller must hold c.mu or accept a transient view.
func (c *LocalConnection) turnContext() *hook.Context {
	if c.currentTurnCtx != nil {
		return c.currentTurnCtx
	}
	return hook.NewTurnContext(c.hookRunner.SessionContext())
}

// ErrConcurrentReceive reports a second concurrent ReceiveSteps call.
var ErrConcurrentReceive = errors.New("local: concurrent ReceiveSteps calls are not supported")

// Compile-time check that LocalConnection satisfies the Connection interface.
var _ connection.Connection = (*LocalConnection)(nil)

// reading helpers below are implemented in reader.go to keep this file focused.

// makeStepKeyFromUpdate builds a stepKey from a StepUpdate.
func makeStepKeyFromUpdate(s *pb.StepUpdate) stepKey {
	return stepKey{trajectoryID: s.GetTrajectoryId(), stepIndex: s.GetStepIndex()}
}

// idStr formats a step key's index for diagnostics.
func (k stepKey) String() string {
	return k.trajectoryID + ":" + strconv.FormatUint(uint64(k.stepIndex), 10)
}
