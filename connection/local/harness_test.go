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

// In-process wire test harness: the Go counterpart of the upstream Python
// connections/local/test_utils.py (TestWebSocket + TestLocalHarness). The fake
// shuttles real protojson bytes through the same encoder/decoder the
// production reader loop uses, so wire-shape behavior is exercised end to end
// without spawning the localharness binary.

package local

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/zchee/antigravity-sdk-go/hook"
	pb "github.com/zchee/antigravity-sdk-go/internal/localharnesspb"
	"github.com/zchee/antigravity-sdk-go/tool"
)

// fakeWS is a wsConn that lets a test inject messages "from the harness" and
// inspect messages "from the SDK". A closed incoming channel makes Read return
// a normal-closure error, mirroring a clean websocket shutdown.
type fakeWS struct {
	incoming chan []byte // harness -> SDK
	sent     chan []byte // SDK   -> harness
	closed   chan struct{}
}

func newFakeWS() *fakeWS {
	return &fakeWS{
		incoming: make(chan []byte, 16),
		sent:     make(chan []byte, 16),
		closed:   make(chan struct{}),
	}
}

// Read blocks for the next harness-sent message; returns a close error when the
// incoming channel is drained and closed (the fake's "harness closed" signal).
func (w *fakeWS) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case msg, ok := <-w.incoming:
		if !ok {
			return 0, nil, &websocket.CloseError{Code: websocket.StatusNormalClosure}
		}
		return websocket.MessageText, msg, nil
	}
}

// Write records the SDK-side message; the test inspects it via sent.
func (w *fakeWS) Write(ctx context.Context, _ websocket.MessageType, data []byte) error {
	cp := append([]byte(nil), data...)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case w.sent <- cp:
		return nil
	}
}

// Close marks the websocket closed. It is safe to call multiple times.
func (w *fakeWS) Close(websocket.StatusCode, string) error {
	select {
	case <-w.closed:
	default:
		close(w.closed)
	}
	return nil
}

// closeFromHarness simulates the harness closing the connection by closing the
// incoming channel — the reader loop's next Read will see a normal-closure.
func (w *fakeWS) closeFromHarness() { close(w.incoming) }

// testHarness wraps a LocalConnection backed by a fakeWS, plus convenience
// helpers for sending OutputEvents and inspecting InputEvent responses.
type testHarness struct {
	t    *testing.T
	ws   *fakeWS
	conn *LocalConnection
}

// newTestHarness builds a LocalConnection over a fakeWS without spawning a
// process. tr and hr may be nil.
func newTestHarness(t *testing.T, tr *tool.Runner, hr *hook.Runner) *testHarness {
	t.Helper()
	ws := newFakeWS()
	conn := newLocalConnection(ws, nil, nil, tr, hr, newStderrBuffer(noopReader{}, 10))
	t.Cleanup(func() {
		// Stop the reader goroutine and any in-flight background dispatches.
		conn.readerCancel()
		conn.bgWG.Wait()
	})
	return &testHarness{t: t, ws: ws, conn: conn}
}

// sendEvent marshals ev as protojson and pushes it to the SDK.
func (h *testHarness) sendEvent(ev *pb.OutputEvent) {
	h.t.Helper()
	data, err := protojson.Marshal(ev)
	if err != nil {
		h.t.Fatalf("marshal OutputEvent: %v", err)
	}
	select {
	case h.ws.incoming <- data:
	case <-time.After(time.Second):
		h.t.Fatal("send timed out (incoming queue full)")
	}
}

// sendStep wraps a StepUpdate in an OutputEvent and sends it.
func (h *testHarness) sendStep(su *pb.StepUpdate) {
	h.sendEvent(pb.OutputEvent_builder{StepUpdate: su}.Build())
}

// sendTrajectoryState wraps a TrajectoryStateUpdate in an OutputEvent.
func (h *testHarness) sendTrajectoryState(tsu *pb.TrajectoryStateUpdate) {
	h.sendEvent(pb.OutputEvent_builder{TrajectoryStateUpdate: tsu}.Build())
}

// sendToolCall wraps a host-tool ToolCall in an OutputEvent.
func (h *testHarness) sendToolCall(id, name, argsJSON string) {
	tc := pb.ToolCall_builder{
		Id:            proto.String(id),
		Name:          proto.String(name),
		ArgumentsJson: proto.String(argsJSON),
	}.Build()
	h.sendEvent(pb.OutputEvent_builder{ToolCall: tc}.Build())
}

// waitForResponse pops the next SDK-side message, unmarshals it as an
// InputEvent, and returns it. It fails the test on timeout or parse error.
func (h *testHarness) waitForResponse(timeout time.Duration) *pb.InputEvent {
	h.t.Helper()
	select {
	case data := <-h.ws.sent:
		var ev pb.InputEvent
		if err := protojson.Unmarshal(data, &ev); err != nil {
			h.t.Fatalf("parse InputEvent: %v\nraw: %s", err, data)
		}
		return &ev
	case <-time.After(timeout):
		h.t.Fatalf("no SDK response within %v", timeout)
		return nil
	}
}

// expectNoResponse asserts no SDK-side message arrives within the timeout. Use
// after a request that should be debounced or ignored.
func (h *testHarness) expectNoResponse(timeout time.Duration) {
	h.t.Helper()
	select {
	case data := <-h.ws.sent:
		var ev pb.InputEvent
		_ = protojson.Unmarshal(data, &ev)
		h.t.Fatalf("unexpected SDK response: %+v", &ev)
	case <-time.After(timeout):
	}
}

// drainSteps consumes ReceiveSteps in a goroutine, collecting steps until it
// ends. It returns a function that blocks until iteration finishes and returns
// the collected steps and any terminal error.
func (h *testHarness) drainSteps(ctx context.Context) func() ([]any, error) {
	type pair struct {
		steps []any
		err   error
	}
	done := make(chan pair, 1)
	go func() {
		var steps []any
		var lastErr error
		for s, err := range h.conn.ReceiveSteps(ctx) {
			if err != nil {
				lastErr = err
				break
			}
			steps = append(steps, s)
		}
		done <- pair{steps, lastErr}
	}()
	return func() ([]any, error) {
		select {
		case p := <-done:
			return p.steps, p.err
		case <-time.After(2 * time.Second):
			h.t.Fatal("drainSteps did not finish")
			return nil, errors.New("timeout")
		}
	}
}

// signalIdleTo flips the connection idle by sending a TrajectoryStateUpdate for
// the parent trajectory (cascadeID); the reader loop must have seen at least
// one step with this cascade_id first or the parent/subagent classification is
// wrong.
func (h *testHarness) signalIdleTo(trajID string) {
	h.sendTrajectoryState(pb.TrajectoryStateUpdate_builder{
		TrajectoryId: proto.String(trajID),
		State:        pb.TrajectoryStateUpdate_STATE_IDLE.Enum(),
	}.Build())
}

// noopReader satisfies io.Reader for stderr; the in-process tests don't need
// real stderr.
type noopReader struct{}

func (noopReader) Read([]byte) (int, error) { return 0, errors.New("eof") }
