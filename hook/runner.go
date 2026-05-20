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

package hook

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/zchee/antigravity-sdk-go/agtypes"
)

// Runner manages collections of the concrete hook types and dispatches
// lifecycle events to them. It is the Go analog of the upstream HookRunner.
//
// A Runner is not safe for concurrent registration and dispatch; register all
// hooks before the session starts dispatching.
type Runner struct {
	onSessionStart    []OnSessionStart
	onSessionEnd      []OnSessionEnd
	preTurn           []PreTurn
	postTurn          []PostTurn
	preToolCallDecide []PreToolCallDecide
	postToolCall      []PostToolCall
	onToolError       []OnToolError
	onInteraction     []OnInteraction
	onCompaction      []OnCompaction
	sessionContext    *Context
}

// NewRunner returns an empty Runner with a fresh session context. Register
// hooks with Register.
func NewRunner() *Runner {
	return &Runner{sessionContext: NewSessionContext()}
}

// SessionContext returns the runner's session-scoped context.
func (r *Runner) SessionContext() *Context { return r.sessionContext }

// HasHooks reports whether any hooks are registered.
func (r *Runner) HasHooks() bool {
	return len(r.onSessionStart) > 0 ||
		len(r.onSessionEnd) > 0 ||
		len(r.preTurn) > 0 ||
		len(r.postTurn) > 0 ||
		len(r.preToolCallDecide) > 0 ||
		len(r.postToolCall) > 0 ||
		len(r.onToolError) > 0 ||
		len(r.onInteraction) > 0 ||
		len(r.onCompaction) > 0
}

// HasPreToolCallDecide reports whether any pre-tool-call decide hook is
// registered. The Agent uses this as the escape hatch for its safety-policy
// guard: a user who registered a decide hook is presumed to be gating tool
// calls themselves.
func (r *Runner) HasPreToolCallDecide() bool {
	return len(r.preToolCallDecide) > 0
}

// ErrUnknownHook reports a hook value whose dynamic type is not one of the
// concrete hook types.
type ErrUnknownHook struct{ Hook Hook }

func (e *ErrUnknownHook) Error() string {
	return fmt.Sprintf("hook: unknown hook type %T", e.Hook)
}

// Register adds a hook, routing it to the appropriate collection by its dynamic
// type. It returns an *ErrUnknownHook if the value is not a concrete hook type.
func (r *Runner) Register(h Hook) error {
	switch v := h.(type) {
	case OnSessionStart:
		r.onSessionStart = append(r.onSessionStart, v)
	case OnSessionEnd:
		r.onSessionEnd = append(r.onSessionEnd, v)
	case PreTurn:
		r.preTurn = append(r.preTurn, v)
	case PostTurn:
		r.postTurn = append(r.postTurn, v)
	case PreToolCallDecide:
		r.preToolCallDecide = append(r.preToolCallDecide, v)
	case PostToolCall:
		r.postToolCall = append(r.postToolCall, v)
	case OnToolError:
		r.onToolError = append(r.onToolError, v)
	case OnInteraction:
		r.onInteraction = append(r.onInteraction, v)
	case OnCompaction:
		r.onCompaction = append(r.onCompaction, v)
	default:
		return &ErrUnknownHook{Hook: h}
	}
	return nil
}

// DispatchSessionStart runs every session-start hook in registration order.
func (r *Runner) DispatchSessionStart(ctx context.Context) error {
	for _, h := range r.onSessionStart {
		if err := h(ctx, r.sessionContext); err != nil {
			return err
		}
	}
	return nil
}

// DispatchSessionEnd runs every session-end hook in registration order.
func (r *Runner) DispatchSessionEnd(ctx context.Context) error {
	for _, h := range r.onSessionEnd {
		if err := h(ctx, r.sessionContext); err != nil {
			return err
		}
	}
	return nil
}

// DispatchPreTurn runs the pre-turn decide hooks against prompt, short-circuiting
// on the first denial. It returns the deciding HookResult (allow by default)
// and a fresh turn context to thread through the rest of the turn.
func (r *Runner) DispatchPreTurn(ctx context.Context, prompt agtypes.Content) (agtypes.HookResult, *Context, error) {
	turn := NewTurnContext(r.sessionContext)
	for _, h := range r.preTurn {
		res, err := h(ctx, turn, prompt)
		if err != nil {
			return agtypes.HookResult{}, turn, err
		}
		if !res.Allow {
			return res, turn, nil
		}
	}
	return agtypes.AllowHookResult(), turn, nil
}

// DispatchPostTurn runs every post-turn hook with the model response.
func (r *Runner) DispatchPostTurn(ctx context.Context, turn *Context, response string) error {
	for _, h := range r.postTurn {
		if err := h(ctx, turn, response); err != nil {
			return err
		}
	}
	return nil
}

// DispatchPreToolCall runs the pre-tool-call decide hooks against call,
// short-circuiting on the first denial. It returns the deciding HookResult, the
// (possibly unchanged) call, and a fresh operation context.
func (r *Runner) DispatchPreToolCall(ctx context.Context, turn *Context, call agtypes.ToolCall) (agtypes.HookResult, agtypes.ToolCall, *Context, error) {
	op := NewOperationContext(turn)
	for _, h := range r.preToolCallDecide {
		res, err := h(ctx, op, call)
		if err != nil {
			return agtypes.HookResult{}, call, op, err
		}
		if !res.Allow {
			return res, call, op, nil
		}
	}
	return agtypes.AllowHookResult(), call, op, nil
}

// DispatchPostToolCall runs every post-tool-call hook with the result.
func (r *Runner) DispatchPostToolCall(ctx context.Context, op *Context, result agtypes.ToolResult) error {
	for _, h := range r.postToolCall {
		if err := h(ctx, op, result); err != nil {
			return err
		}
	}
	return nil
}

// DispatchOnToolError runs the tool-error hooks until one handles the error.
//
// It mirrors the upstream semantics: the first hook that returns a replacement
// (handled=true) wins, yielding an allowing HookResult and that replacement. A
// hook that itself errors produces a denying HookResult describing the recovery
// failure. If no hook handles the error, the result denies with a nil
// replacement.
func (r *Runner) DispatchOnToolError(ctx context.Context, op *Context, toolErr error) (agtypes.HookResult, any, error) {
	for _, h := range r.onToolError {
		replacement, handled, err := h(ctx, op, toolErr)
		if err != nil {
			slog.Error("critical failure in OnToolError hook", "err", err)
			return agtypes.HookResult{Allow: false, Message: fmt.Sprintf("Error recovery failed: %v", err)}, nil, nil
		}
		if handled {
			return agtypes.HookResult{Allow: true}, replacement, nil
		}
	}
	return agtypes.HookResult{Allow: false}, nil, nil
}

// DispatchInteraction runs the interaction hooks until one handles the request.
//
// The first hook that reports handled=true wins, yielding an allowing
// HookResult and its response. If none handles it, the result denies.
func (r *Runner) DispatchInteraction(ctx context.Context, turn *Context, spec agtypes.AskQuestionInteractionSpec) (agtypes.HookResult, agtypes.QuestionHookResult, *Context, error) {
	op := NewOperationContext(turn)
	for _, h := range r.onInteraction {
		res, handled, err := h(ctx, op, spec)
		if err != nil {
			return agtypes.HookResult{}, agtypes.QuestionHookResult{}, op, err
		}
		if handled {
			return agtypes.HookResult{Allow: true}, res, op, nil
		}
	}
	return agtypes.HookResult{Allow: false, Message: "No interaction hook handled the request"}, agtypes.QuestionHookResult{}, op, nil
}

// DispatchCompaction runs every compaction hook with the event data, using a
// fresh operation context derived from turn.
func (r *Runner) DispatchCompaction(ctx context.Context, turn *Context, data any) error {
	op := NewOperationContext(turn)
	for _, h := range r.onCompaction {
		if err := h(ctx, op, data); err != nil {
			return err
		}
	}
	return nil
}
