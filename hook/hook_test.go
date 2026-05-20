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

package hook_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/hook"
)

func TestContextChainGet(t *testing.T) {
	session := hook.NewSessionContext()
	session.Set("a", 1)
	turn := hook.NewTurnContext(session)
	turn.Set("b", 2)
	op := hook.NewOperationContext(turn)
	op.Set("a", 10) // shadows session "a"

	tests := map[string]struct {
		ctx   *hook.Context
		key   string
		want  any
		found bool
	}{
		"local value":            {ctx: op, key: "a", want: 10, found: true},
		"inherited from turn":    {ctx: op, key: "b", want: 2, found: true},
		"session value via turn": {ctx: turn, key: "a", want: 1, found: true},
		"missing key":            {ctx: op, key: "missing", want: nil, found: false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, found := tc.ctx.Get(tc.key)
			if found != tc.found || got != tc.want {
				t.Errorf("Get(%q) = (%v, %v), want (%v, %v)", tc.key, got, found, tc.want, tc.found)
			}
		})
	}
}

func TestDispatchSessionStartOrder(t *testing.T) {
	r := hook.NewRunner()
	var order []int
	for i := range 3 {
		if err := r.Register(hook.OnSessionStart(func(context.Context, *hook.Context) error {
			order = append(order, i)
			return nil
		})); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.DispatchSessionStart(t.Context()); err != nil {
		t.Fatalf("DispatchSessionStart: %v", err)
	}
	want := []int{0, 1, 2}
	if len(order) != 3 || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] {
		t.Errorf("dispatch order = %v, want %v", order, want)
	}
}

func TestDispatchSessionStartError(t *testing.T) {
	r := hook.NewRunner()
	sentinel := errors.New("boom")
	var ran int
	_ = r.Register(hook.OnSessionStart(func(context.Context, *hook.Context) error {
		ran++
		return sentinel
	}))
	_ = r.Register(hook.OnSessionStart(func(context.Context, *hook.Context) error {
		ran++
		return nil
	}))
	err := r.DispatchSessionStart(t.Context())
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want %v", err, sentinel)
	}
	if ran != 1 {
		t.Errorf("ran %d hooks, want 1 (short-circuit on error)", ran)
	}
}

func TestDispatchPreTurnShortCircuit(t *testing.T) {
	r := hook.NewRunner()
	var ran int
	_ = r.Register(hook.PreTurn(func(context.Context, *hook.Context, agtypes.Content) (agtypes.HookResult, error) {
		ran++
		return agtypes.AllowHookResult(), nil
	}))
	_ = r.Register(hook.PreTurn(func(context.Context, *hook.Context, agtypes.Content) (agtypes.HookResult, error) {
		ran++
		return agtypes.HookResult{Allow: false, Message: "denied"}, nil
	}))
	_ = r.Register(hook.PreTurn(func(context.Context, *hook.Context, agtypes.Content) (agtypes.HookResult, error) {
		ran++
		return agtypes.AllowHookResult(), nil
	}))

	res, turn, err := r.DispatchPreTurn(t.Context(), "hello")
	if err != nil {
		t.Fatalf("DispatchPreTurn: %v", err)
	}
	if res.Allow {
		t.Error("result allowed, want denied")
	}
	if res.Message != "denied" {
		t.Errorf("message = %q, want denied", res.Message)
	}
	if ran != 2 {
		t.Errorf("ran %d hooks, want 2 (short-circuit on denial)", ran)
	}
	if turn == nil || turn.Parent() != r.SessionContext() {
		t.Error("turn context not parented to session context")
	}
}

func TestDispatchPreTurnAllow(t *testing.T) {
	r := hook.NewRunner()
	res, _, err := r.DispatchPreTurn(t.Context(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Allow {
		t.Error("empty runner should allow by default")
	}
}

func TestDispatchOnToolError(t *testing.T) {
	toolErr := errors.New("tool blew up")
	tests := map[string]struct {
		hooks       []hook.OnToolError
		wantAllow   bool
		wantReplace any
	}{
		"no hooks denies": {
			hooks:     nil,
			wantAllow: false,
		},
		"first handler wins": {
			hooks: []hook.OnToolError{
				func(context.Context, *hook.Context, error) (any, bool, error) { return "recovered", true, nil },
				func(context.Context, *hook.Context, error) (any, bool, error) { return "second", true, nil },
			},
			wantAllow:   true,
			wantReplace: "recovered",
		},
		"unhandled falls through to deny": {
			hooks: []hook.OnToolError{
				func(context.Context, *hook.Context, error) (any, bool, error) { return nil, false, nil },
			},
			wantAllow: false,
		},
		"hook error denies with message": {
			hooks: []hook.OnToolError{
				func(context.Context, *hook.Context, error) (any, bool, error) {
					return nil, false, errors.New("recovery failed")
				},
			},
			wantAllow: false,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			r := hook.NewRunner()
			for _, h := range tc.hooks {
				if err := r.Register(h); err != nil {
					t.Fatal(err)
				}
			}
			turn := hook.NewTurnContext(r.SessionContext())
			op := hook.NewOperationContext(turn)
			res, replace, err := r.DispatchOnToolError(t.Context(), op, toolErr)
			if err != nil {
				t.Fatalf("DispatchOnToolError: %v", err)
			}
			if res.Allow != tc.wantAllow {
				t.Errorf("allow = %v, want %v", res.Allow, tc.wantAllow)
			}
			if replace != tc.wantReplace {
				t.Errorf("replacement = %v, want %v", replace, tc.wantReplace)
			}
		})
	}
}

func TestHasHooks(t *testing.T) {
	r := hook.NewRunner()
	if r.HasHooks() {
		t.Error("empty runner reports HasHooks")
	}
	_ = r.Register(hook.OnCompaction(func(context.Context, *hook.Context, any) error { return nil }))
	if !r.HasHooks() {
		t.Error("runner with a hook reports no hooks")
	}
}
