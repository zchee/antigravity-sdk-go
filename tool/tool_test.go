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

package tool_test

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/tool"
)

// fakeConn is a stub satisfying the narrow connection interface ToolContext
// depends on.
type fakeConn struct {
	id      string
	idle    bool
	sent    []string
	sendErr error
}

func (f *fakeConn) ConversationID() string { return f.id }
func (f *fakeConn) IsIdle() bool           { return f.idle }
func (f *fakeConn) SendTriggerNotification(_ context.Context, msg string) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	f.sent = append(f.sent, msg)
	return nil
}

func TestRegisterAndExecute(t *testing.T) {
	r := tool.NewRunner()
	if err := r.Register("echo", func(_ context.Context, args map[string]any) (any, error) {
		return args["v"], nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := r.Execute(t.Context(), "echo", map[string]any{"v": "hi"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got != "hi" {
		t.Errorf("Execute = %v, want hi", got)
	}
}

func TestRegisterDuplicate(t *testing.T) {
	r := tool.NewRunner()
	_ = r.Register("t", func(context.Context, map[string]any) (any, error) { return nil, nil })
	err := r.Register("t", func(context.Context, map[string]any) (any, error) { return nil, nil })
	var dup *tool.ErrToolAlreadyRegistered
	if !errors.As(err, &dup) {
		t.Errorf("duplicate Register error = %v, want *ErrToolAlreadyRegistered", err)
	}
}

func TestUnregister(t *testing.T) {
	r := tool.NewRunner()
	_ = r.Register("t", func(context.Context, map[string]any) (any, error) { return nil, nil })
	if err := r.Unregister("t"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	err := r.Unregister("t")
	var notReg *tool.ErrToolNotRegistered
	if !errors.As(err, &notReg) {
		t.Errorf("Unregister(absent) error = %v, want *ErrToolNotRegistered", err)
	}
}

func TestExecuteUnregistered(t *testing.T) {
	r := tool.NewRunner()
	_, err := r.Execute(t.Context(), "nope", nil)
	var notReg *tool.ErrToolNotRegistered
	if !errors.As(err, &notReg) {
		t.Errorf("Execute(absent) error = %v, want *ErrToolNotRegistered", err)
	}
}

func TestNamesAndSchema(t *testing.T) {
	r := tool.NewRunner()
	_ = r.Register("plain", func(context.Context, map[string]any) (any, error) { return nil, nil })
	schema := map[string]any{"type": "object"}
	_ = r.RegisterWithSchema("schemad", tool.ToolWithSchema{
		Fn:          func(context.Context, map[string]any) (any, error) { return nil, nil },
		InputSchema: schema,
	})

	names := r.Names()
	slices.Sort(names)
	if !slices.Equal(names, []string{"plain", "schemad"}) {
		t.Errorf("Names() = %v, want [plain schemad]", names)
	}
	if _, ok := r.Schema("plain"); ok {
		t.Error("plain tool unexpectedly has a schema")
	}
	got, ok := r.Schema("schemad")
	if !ok || got["type"] != "object" {
		t.Errorf("Schema(schemad) = (%v, %v), want object schema", got, ok)
	}
	// Schema returns a clone: mutating it must not affect the registry.
	got["type"] = "mutated"
	again, _ := r.Schema("schemad")
	if again["type"] != "object" {
		t.Error("Schema did not return a defensive copy")
	}
}

func TestToolContextInjection(t *testing.T) {
	r := tool.NewRunner()
	fc := &fakeConn{id: "conv-1", idle: true}
	r.SetContext(tool.NewToolContext(fc))

	if err := r.Register("needs_ctx", func(ctx context.Context, _ map[string]any) (any, error) {
		tc, ok := tool.FromContext(ctx)
		if !ok {
			return nil, errors.New("no ToolContext in ctx")
		}
		tc.SetState("k", "v")
		return tc.ConversationID(), nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := r.Execute(t.Context(), "needs_ctx", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got != "conv-1" {
		t.Errorf("conversation id = %v, want conv-1", got)
	}
}

func TestToolContextStateAndSend(t *testing.T) {
	fc := &fakeConn{id: "c", idle: false}
	tc := tool.NewToolContext(fc)
	if tc.IsIdle() {
		t.Error("IsIdle = true, want false")
	}
	if _, ok := tc.GetState("missing"); ok {
		t.Error("GetState(missing) found a value")
	}
	tc.SetState("x", 42)
	if v, ok := tc.GetState("x"); !ok || v != 42 {
		t.Errorf("GetState(x) = (%v, %v), want (42, true)", v, ok)
	}
	if err := tc.Send(t.Context(), "ping"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(fc.sent) != 1 || fc.sent[0] != "ping" {
		t.Errorf("sent = %v, want [ping]", fc.sent)
	}
}

func TestProcessToolCalls(t *testing.T) {
	r := tool.NewRunner()
	var calls atomic.Int64
	_ = r.Register("ok", func(_ context.Context, args map[string]any) (any, error) {
		calls.Add(1)
		return args["n"], nil
	})
	_ = r.Register("fail", func(context.Context, map[string]any) (any, error) {
		calls.Add(1)
		return nil, errors.New("nope")
	})

	in := []agtypes.ToolCall{
		{Name: "ok", ID: "1", Args: map[string]any{"n": 1}},
		{Name: "fail", ID: "2"},
		{Name: "unknown", ID: "3"},
		{Name: "ok", ID: "4", Args: map[string]any{"n": 4}},
	}
	out := r.ProcessToolCalls(t.Context(), in)
	if len(out) != len(in) {
		t.Fatalf("got %d results, want %d", len(out), len(in))
	}
	// Order is preserved.
	if out[0].Result != 1 || out[0].ID != "1" {
		t.Errorf("result[0] = %+v, want result 1 id 1", out[0])
	}
	if out[1].Error == "" || out[1].Exception == nil {
		t.Errorf("result[1] should carry the tool error: %+v", out[1])
	}
	var notReg *tool.ErrToolNotRegistered
	if !errors.As(out[2].Exception, &notReg) {
		t.Errorf("result[2] exception = %v, want *ErrToolNotRegistered", out[2].Exception)
	}
	if out[3].Result != 4 {
		t.Errorf("result[3] = %+v, want result 4", out[3])
	}
	if calls.Load() != 3 {
		t.Errorf("tool body ran %d times, want 3 (unknown never runs)", calls.Load())
	}
}
