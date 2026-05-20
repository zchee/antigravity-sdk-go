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

package policy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/hook"
	"github.com/zchee/antigravity-sdk-go/hook/policy"
)

// decide runs an enforced policy set against a single tool call and returns the
// result, failing the test on a construction or dispatch error.
func decide(t *testing.T, policies []policy.Policy, call agtypes.ToolCall) agtypes.HookResult {
	t.Helper()
	h, err := policy.Enforce(policies)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	res, err := h(t.Context(), hook.NewSessionContext(), call)
	if err != nil {
		t.Fatalf("hook: %v", err)
	}
	return res
}

func approve(context.Context, agtypes.ToolCall) (bool, error) { return true, nil }
func reject(context.Context, agtypes.ToolCall) (bool, error)  { return false, nil }

func TestEnforceDefaultOpen(t *testing.T) {
	res := decide(t, nil, agtypes.ToolCall{Name: "anything"})
	if !res.Allow {
		t.Error("empty policy set should allow by default")
	}
}

func TestEnforcePriority(t *testing.T) {
	tests := map[string]struct {
		policies  []policy.Policy
		call      agtypes.ToolCall
		wantAllow bool
	}{
		"specific deny beats wildcard allow": {
			policies:  []policy.Policy{policy.AllowAll(), policy.DenyTool("run_command", nil, "")},
			call:      agtypes.ToolCall{Name: "run_command"},
			wantAllow: false,
		},
		"specific allow beats wildcard deny": {
			policies:  []policy.Policy{policy.DenyAll(), policy.Allow("view_file", nil, "")},
			call:      agtypes.ToolCall{Name: "view_file"},
			wantAllow: true,
		},
		"wildcard deny applies to unmatched specific": {
			policies:  []policy.Policy{policy.DenyAll(), policy.Allow("view_file", nil, "")},
			call:      agtypes.ToolCall{Name: "edit_file"},
			wantAllow: false,
		},
		"specific ask beats specific allow": {
			// A rejecting ask handler must win over a co-located allow: if allow
			// outranked ask the call would be permitted. The denial proves ask is
			// consulted first.
			policies: []policy.Policy{
				policy.Allow("run_command", nil, ""),
				policy.Ask("run_command", reject, nil, ""),
			},
			call:      agtypes.ToolCall{Name: "run_command"},
			wantAllow: false,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := decide(t, tc.policies, tc.call).Allow; got != tc.wantAllow {
				t.Errorf("Allow = %v, want %v", got, tc.wantAllow)
			}
		})
	}
}

func TestAskUserApproveDeny(t *testing.T) {
	tests := map[string]struct {
		handler   policy.AskUserHandler
		wantAllow bool
	}{
		"approved": {handler: approve, wantAllow: true},
		"rejected": {handler: reject, wantAllow: false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			policies := []policy.Policy{policy.Ask("run_command", tc.handler, nil, "confirm")}
			res := decide(t, policies, agtypes.ToolCall{Name: "run_command"})
			if res.Allow != tc.wantAllow {
				t.Errorf("Allow = %v, want %v", res.Allow, tc.wantAllow)
			}
		})
	}
}

func TestPredicateGating(t *testing.T) {
	// Deny run_command only when args contain a dangerous flag.
	dangerous := func(_ context.Context, tc agtypes.ToolCall) (bool, error) {
		cmd, _ := tc.Args["CommandLine"].(string)
		return cmd == "rm -rf /", nil
	}
	policies := []policy.Policy{
		policy.DenyTool("run_command", dangerous, "no_rm"),
		policy.AllowAll(),
	}
	if decide(t, policies, agtypes.ToolCall{Name: "run_command", Args: map[string]any{"CommandLine": "ls"}}).Allow != true {
		t.Error("safe command should be allowed")
	}
	if decide(t, policies, agtypes.ToolCall{Name: "run_command", Args: map[string]any{"CommandLine": "rm -rf /"}}).Allow != false {
		t.Error("dangerous command should be denied")
	}
}

func TestFailClosed(t *testing.T) {
	boom := errors.New("predicate exploded")
	tests := map[string]struct {
		when policy.Predicate
	}{
		"predicate error": {when: func(context.Context, agtypes.ToolCall) (bool, error) { return false, boom }},
		"predicate panic": {when: func(context.Context, agtypes.ToolCall) (bool, error) { panic("kaboom") }},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			policies := []policy.Policy{policy.DenyTool("run_command", tc.when, "risky"), policy.AllowAll()}
			res := decide(t, policies, agtypes.ToolCall{Name: "run_command"})
			if res.Allow {
				t.Error("evaluation failure should fail closed (deny)")
			}
			if res.Message == "" {
				t.Error("fail-closed denial should carry a message")
			}
		})
	}
}

func TestEnforceMissingHandler(t *testing.T) {
	_, err := policy.Enforce([]policy.Policy{
		{Tool: "run_command", Decision: policy.AskUser, Name: "bad"},
	})
	if !errors.Is(err, policy.ErrMissingAskUserHandler) {
		t.Errorf("Enforce error = %v, want ErrMissingAskUserHandler", err)
	}
}

func TestConfirmRunCommand(t *testing.T) {
	t.Run("no handler denies run_command", func(t *testing.T) {
		policies := policy.ConfirmRunCommand(nil)
		if decide(t, policies, agtypes.ToolCall{Name: "run_command"}).Allow {
			t.Error("run_command should be denied without a handler")
		}
		if !decide(t, policies, agtypes.ToolCall{Name: "view_file"}).Allow {
			t.Error("non-run_command tools should be allowed")
		}
	})
	t.Run("handler asks for run_command", func(t *testing.T) {
		policies := policy.ConfirmRunCommand(approve)
		if !decide(t, policies, agtypes.ToolCall{Name: "run_command"}).Allow {
			t.Error("run_command should be allowed when handler approves")
		}
	})
}

func TestSafeDefaults(t *testing.T) {
	policies := policy.SafeDefaults(reject)
	// Read-only tools allowed.
	if !decide(t, policies, agtypes.ToolCall{Name: string(agtypes.ToolViewFile)}).Allow {
		t.Error("read-only view_file should be allowed")
	}
	// Other tools go through ask-user, which rejects.
	if decide(t, policies, agtypes.ToolCall{Name: string(agtypes.ToolRunCommand)}).Allow {
		t.Error("non-read-only tool should be denied by the rejecting handler")
	}
}

func TestWorkspaceOnly(t *testing.T) {
	ws := t.TempDir()
	policies := append(policy.WorkspaceOnly([]string{ws}), policy.AllowAll())

	tests := map[string]struct {
		call      agtypes.ToolCall
		wantAllow bool
	}{
		"file tool inside workspace": {
			call:      agtypes.ToolCall{Name: string(agtypes.ToolEditFile), CanonicalPath: ws + "/f.txt"},
			wantAllow: true,
		},
		"file tool outside workspace": {
			call:      agtypes.ToolCall{Name: string(agtypes.ToolEditFile), CanonicalPath: "/etc/passwd"},
			wantAllow: false,
		},
		"file tool with no path is denied": {
			// A file operation that cannot be proven in-workspace must not be
			// permitted by the sandbox (fail closed, not open).
			call:      agtypes.ToolCall{Name: string(agtypes.ToolViewFile)},
			wantAllow: false,
		},
		"non-file tool unaffected": {
			call:      agtypes.ToolCall{Name: string(agtypes.ToolRunCommand), CanonicalPath: "/etc/passwd"},
			wantAllow: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := decide(t, policies, tc.call).Allow; got != tc.wantAllow {
				t.Errorf("Allow = %v, want %v", got, tc.wantAllow)
			}
		})
	}
}
