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

package interactive_test

import (
	"context"
	"testing"

	ag "github.com/zchee/antigravity-sdk-go"
	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/connection"
	"github.com/zchee/antigravity-sdk-go/hook"
	"github.com/zchee/antigravity-sdk-go/hook/policy"
	"github.com/zchee/antigravity-sdk-go/interactive"
)

// scriptPrompter returns the queued lines in order, then ErrInputClosed.
type scriptPrompter struct {
	lines []string
	i     int
}

func (s *scriptPrompter) Prompt(context.Context, string) (string, error) {
	if s.i >= len(s.lines) {
		return "", interactive.ErrInputClosed
	}
	line := s.lines[s.i]
	s.i++
	return line, nil
}

func TestToolConfirmationHook(t *testing.T) {
	tests := map[string]struct {
		answer    string
		closed    bool
		wantAllow bool
	}{
		"yes allows":    {answer: "y", wantAllow: true},
		"yes word":      {answer: "Yes", wantAllow: true},
		"no denies":     {answer: "n", wantAllow: false},
		"other denies":  {answer: "maybe", wantAllow: false},
		"closed denies": {closed: true, wantAllow: false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var p interactive.Prompter
			if tc.closed {
				p = &scriptPrompter{}
			} else {
				p = &scriptPrompter{lines: []string{tc.answer}}
			}
			h := interactive.NewToolConfirmationHook(p)
			res, err := h(t.Context(), hook.NewSessionContext(), agtypes.ToolCall{Name: "run_command"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Allow != tc.wantAllow {
				t.Errorf("Allow = %v, want %v", res.Allow, tc.wantAllow)
			}
		})
	}
}

func TestAskQuestionHook(t *testing.T) {
	spec := agtypes.AskQuestionInteractionSpec{
		Questions: []agtypes.AskQuestionEntry{{
			Question: "Pick",
			Options: []agtypes.AskQuestionOption{
				{ID: "opt-a", Text: "Alpha"},
				{ID: "opt-b", Text: "Beta"},
			},
		}},
	}
	tests := map[string]struct {
		answer       string
		wantSelected []string
		wantFreeform string
		wantSkipped  bool
	}{
		"numeric selects by position": {answer: "2", wantSelected: []string{"opt-b"}},
		"text match selects":          {answer: "alpha", wantSelected: []string{"opt-a"}},
		"id match selects":            {answer: "opt-b", wantSelected: []string{"opt-b"}},
		"empty skips":                 {answer: "", wantSkipped: true},
		"freeform fallback":           {answer: "something else", wantFreeform: "something else"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			h := interactive.NewAskQuestionHook(&scriptPrompter{lines: []string{tc.answer}})
			res, handled, err := h(t.Context(), hook.NewSessionContext(), spec)
			if err != nil {
				t.Fatal(err)
			}
			if !handled {
				t.Fatal("interaction not handled")
			}
			if len(res.Responses) != 1 {
				t.Fatalf("got %d responses, want 1", len(res.Responses))
			}
			r := res.Responses[0]
			if tc.wantSkipped && !r.Skipped {
				t.Errorf("response = %+v, want skipped", r)
			}
			if tc.wantFreeform != "" && r.FreeformResponse != tc.wantFreeform {
				t.Errorf("freeform = %q, want %q", r.FreeformResponse, tc.wantFreeform)
			}
			if len(tc.wantSelected) > 0 {
				if len(r.SelectedOptionIDs) != 1 || r.SelectedOptionIDs[0] != tc.wantSelected[0] {
					t.Errorf("selected = %v, want %v", r.SelectedOptionIDs, tc.wantSelected)
				}
			}
		})
	}
}

func TestAskQuestionHookCancelOnClose(t *testing.T) {
	spec := agtypes.AskQuestionInteractionSpec{
		Questions: []agtypes.AskQuestionEntry{{Question: "Q"}},
	}
	h := interactive.NewAskQuestionHook(&scriptPrompter{}) // immediately closed
	res, handled, err := h(t.Context(), hook.NewSessionContext(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !res.Cancelled {
		t.Errorf("result = %+v (handled=%v), want cancelled", res, handled)
	}
}

func TestWithUserConfirmation(t *testing.T) {
	cfg := &connection.FakeConfig{}
	cfg.PoliciesValue = []policy.Policy{
		policy.DenyTool(string(agtypes.ToolRunCommand), nil, "confirm_run_command"),
		policy.AllowAll(),
	}
	out := interactive.WithUserConfirmation(cfg, &scriptPrompter{})
	policies := out.Policies()
	if len(policies) != 2 {
		t.Fatalf("got %d policies, want 2", len(policies))
	}
	// The bare deny(run_command) is upgraded to ask_user; AllowAll is unchanged.
	if policies[0].Decision != policy.AskUser {
		t.Errorf("run_command policy decision = %v, want AskUser", policies[0].Decision)
	}
	if policies[0].AskUser == nil {
		t.Error("upgraded policy has no ask-user handler")
	}
	if policies[1].Decision != policy.Approve {
		t.Errorf("second policy = %v, want unchanged AllowAll", policies[1].Decision)
	}
}

func TestWithUserConfirmationLeavesConditionalDeny(t *testing.T) {
	// A deny with a predicate (conditional) is NOT upgraded — only bare denies.
	pred := func(context.Context, agtypes.ToolCall) (bool, error) { return true, nil }
	cfg := &connection.FakeConfig{}
	cfg.PoliciesValue = []policy.Policy{policy.DenyTool(string(agtypes.ToolRunCommand), pred, "rm-guard")}
	out := interactive.WithUserConfirmation(cfg, &scriptPrompter{})
	if out.Policies()[0].Decision != policy.Deny {
		t.Error("conditional deny should not be upgraded")
	}
}

func TestRunInteractiveLoopExitsOnQuit(t *testing.T) {
	cfg := &connection.FakeConfig{Strategy: &connection.FakeStrategy{Conn: &connection.FakeConnection{}}}
	cfg.PoliciesValue = []policy.Policy{policy.AllowAll()}
	agent, err := ag.New(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close(t.Context())

	// "quit" ends the loop before any chat turn.
	if err := interactive.RunInteractiveLoop(t.Context(), agent, &scriptPrompter{lines: []string{"quit"}}); err != nil {
		t.Errorf("RunInteractiveLoop: %v", err)
	}
}

func TestRunInteractiveLoopRequiresStarted(t *testing.T) {
	cfg := &connection.FakeConfig{}
	cfg.PoliciesValue = []policy.Policy{policy.AllowAll()}
	agent := ag.NewAgent(cfg)
	if err := interactive.RunInteractiveLoop(t.Context(), agent, &scriptPrompter{}); err == nil {
		t.Error("RunInteractiveLoop on unstarted agent = nil error, want ErrAgentNotStarted")
	}
}
