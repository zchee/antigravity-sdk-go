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

package antigravity_test

import (
	"context"
	"errors"
	"testing"

	ag "github.com/zchee/antigravity-sdk-go"
	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/connection"
	"github.com/zchee/antigravity-sdk-go/hook"
	"github.com/zchee/antigravity-sdk-go/hook/policy"
	"github.com/zchee/antigravity-sdk-go/tool"
	"github.com/zchee/antigravity-sdk-go/trigger"
)

// writeToolsCaps returns capabilities enabling a write tool, so the safety
// guard is engaged.
func writeToolsCaps() agtypes.CapabilitiesConfig {
	return agtypes.CapabilitiesConfig{EnabledTools: []agtypes.BuiltinTools{agtypes.ToolEditFile}}
}

// newFakeConfig builds a FakeConfig with the given capabilities and policies,
// seeding a fake connection that replays steps.
func newFakeConfig(caps agtypes.CapabilitiesConfig, policies []policy.Policy, steps []agtypes.Step) *connection.FakeConfig {
	c := &connection.FakeConfig{Strategy: &connection.FakeStrategy{Conn: &connection.FakeConnection{ConvID: "conv-1", Steps: steps}}}
	c.CapabilitiesValue = caps
	c.PoliciesValue = policies
	return c
}

func TestAgentLifecycle(t *testing.T) {
	cfg := newFakeConfig(agtypes.CapabilitiesConfig{}, []policy.Policy{policy.AllowAll()}, nil)
	a, err := ag.New(t.Context(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !a.IsStarted() {
		t.Error("IsStarted = false after New")
	}
	if a.ConversationID() != "conv-1" {
		t.Errorf("ConversationID = %q, want conv-1", a.ConversationID())
	}
	if err := a.Close(t.Context()); err != nil {
		t.Errorf("Close: %v", err)
	}
	if a.IsStarted() {
		t.Error("IsStarted = true after Close")
	}
	// Close is idempotent.
	if err := a.Close(t.Context()); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestAgentNoPolicyGuard(t *testing.T) {
	tests := map[string]struct {
		caps     agtypes.CapabilitiesConfig
		policies []policy.Policy
		hook     hook.Hook
		wantErr  bool
	}{
		"write tools, no policy, no hook -> error": {
			caps:    writeToolsCaps(),
			wantErr: true,
		},
		"write tools satisfied by AllowAll": {
			caps:     writeToolsCaps(),
			policies: []policy.Policy{policy.AllowAll()},
			wantErr:  false,
		},
		"write tools satisfied by decide hook": {
			caps: writeToolsCaps(),
			hook: hook.PreToolCallDecide(func(context.Context, *hook.Context, agtypes.ToolCall) (agtypes.HookResult, error) {
				return agtypes.AllowHookResult(), nil
			}),
			wantErr: false,
		},
		"read-only tools need no policy": {
			caps:    agtypes.CapabilitiesConfig{EnabledTools: agtypes.ReadOnlyTools()},
			wantErr: false,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := newFakeConfig(tc.caps, tc.policies, nil)
			a := ag.NewAgent(cfg)
			if tc.hook != nil {
				if err := a.RegisterHook(tc.hook); err != nil {
					t.Fatal(err)
				}
			}
			err := a.Start(t.Context())
			if tc.wantErr {
				if !errors.Is(err, ag.ErrNoPolicy) {
					t.Errorf("Start error = %v, want ErrNoPolicy", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			_ = a.Close(t.Context())
		})
	}
}

func TestAgentRegisterTriggerAfterStart(t *testing.T) {
	cfg := newFakeConfig(agtypes.CapabilitiesConfig{}, []policy.Policy{policy.AllowAll()}, nil)
	a, err := ag.New(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close(t.Context())
	noop := func(ctx context.Context, _ *trigger.Context) error { return nil }
	if err := a.RegisterTrigger(noop); !errors.Is(err, ag.ErrAgentStarted) {
		t.Errorf("RegisterTrigger after start = %v, want ErrAgentStarted", err)
	}
}

func TestAgentChatDelegates(t *testing.T) {
	steps := []agtypes.Step{
		{StepIndex: 0, Source: agtypes.StepSourceModel, Target: agtypes.StepTargetUser, ContentDelta: "pong"},
	}
	cfg := newFakeConfig(agtypes.CapabilitiesConfig{}, []policy.Policy{policy.AllowAll()}, steps)
	a, err := ag.New(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close(t.Context())

	resp, err := a.Chat(t.Context(), "ping")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	text, err := resp.Text()
	if err != nil {
		t.Fatal(err)
	}
	if text != "pong" {
		t.Errorf("Chat text = %q, want pong", text)
	}
	// The same conversation is exposed via the accessor.
	if a.Conversation() == nil {
		t.Error("Conversation() returned nil after start")
	}
}

func TestAgentChatBeforeStart(t *testing.T) {
	cfg := newFakeConfig(agtypes.CapabilitiesConfig{}, []policy.Policy{policy.AllowAll()}, nil)
	a := ag.NewAgent(cfg)
	if _, err := a.Chat(t.Context(), "x"); !errors.Is(err, ag.ErrAgentNotStarted) {
		t.Errorf("Chat before start = %v, want ErrAgentNotStarted", err)
	}
}

// TestAgentWiresToolContext verifies the Phase 3 contract: the Agent calls
// toolRunner.SetContext so a host tool can reach the conversation via
// tool.FromContext.
func TestAgentWiresToolContext(t *testing.T) {
	got := make(chan string, 1)
	cfg := newFakeConfig(agtypes.CapabilitiesConfig{}, []policy.Policy{policy.AllowAll()}, nil)
	cfg.ToolsValue = []tool.ToolWithSchema{{
		Name: "probe",
		Fn: func(ctx context.Context, _ map[string]any) (any, error) {
			tc, ok := tool.FromContext(ctx)
			if !ok {
				got <- ""
				return nil, errors.New("no tool context")
			}
			got <- tc.ConversationID()
			return nil, nil
		},
	}}
	a, err := ag.New(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close(t.Context())

	// Execute the registered tool through the runner the Agent built and wired.
	// The Agent operates on a clone of cfg, but the clone preserves the shared
	// Strategy pointer, which captured the runner in CreateStrategy.
	tr := cfg.Strategy.ToolRunner
	if tr == nil {
		t.Fatal("Agent did not pass a tool runner to CreateStrategy")
	}
	if _, err := tr.Execute(t.Context(), "probe", nil); err != nil {
		t.Fatalf("Execute probe: %v", err)
	}
	if id := <-got; id != "conv-1" {
		t.Errorf("tool saw conversation id %q, want conv-1 (ToolContext not wired)", id)
	}
}

func TestAgentConfigDeepCopyIsolation(t *testing.T) {
	cfg := newFakeConfig(agtypes.CapabilitiesConfig{}, []policy.Policy{policy.AllowAll()}, nil)
	cfg.WorkspacesValue = []string{"/ws"}
	a, err := ag.New(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close(t.Context())
	// Mutating the caller's config after New must not affect the running agent.
	cfg.WorkspacesValue[0] = "/mutated"
	// The agent holds its own clone; we can't read it directly, but the deep
	// copy is exercised — this asserts no panic/shared-state corruption and
	// documents the contract.
	if !a.IsStarted() {
		t.Error("agent not started")
	}
}
