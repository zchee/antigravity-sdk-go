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

// Package hook defines the Antigravity SDK hooks and the runner that
// dispatches their lifecycle events.
//
// Upstream defines three abstract hook kinds parameterized by data type —
// InspectHook[T] (read-only observability), DecideHook[T] (blocking
// allow/deny), and TransformHook[T,R] (blocking data transformation) — and a
// fixed set of concrete hooks built on them. The Go port keeps the same
// taxonomy but represents each concrete hook as a typed function value rather
// than a class hierarchy: Go function types are the natural analog of the
// upstream decorator-wrapped callables, and each concrete hook has a single
// fixed data type, so a union of generics is unnecessary.
//
// All hooks implement the sealed Hook marker interface so that a single
// Runner.Register call can accept any hook and route it by type.
package hook

import (
	"context"

	"github.com/zchee/antigravity-sdk-go/agtypes"
)

// Hook is the sealed marker interface implemented by every concrete hook type.
// It exists so Runner.Register can accept any hook and dispatch it by its
// dynamic type. Use the constructor functions (OnSessionStart, PreTurn, etc.)
// to build values; the set of implementations is closed to this package.
type Hook interface {
	isHook()
}

// Inspect kinds are read-only and non-blocking (observability). Decide kinds
// are read-only and blocking (policy). Transform kinds may modify data and are
// blocking.

// OnSessionStart is invoked when the session starts.
type OnSessionStart func(ctx context.Context, hc *Context) error

func (OnSessionStart) isHook() {}

// OnSessionEnd is invoked when the session ends.
type OnSessionEnd func(ctx context.Context, hc *Context) error

func (OnSessionEnd) isHook() {}

// PreTurn is invoked before a turn starts. It receives the user's prompt and
// returns a HookResult; a result with Allow=false blocks the turn.
type PreTurn func(ctx context.Context, hc *Context, prompt agtypes.Content) (agtypes.HookResult, error)

func (PreTurn) isHook() {}

// PostTurn is invoked after a turn ends. It receives the model's response text.
type PostTurn func(ctx context.Context, hc *Context, response string) error

func (PostTurn) isHook() {}

// PreToolCallDecide is invoked before a tool call to decide whether it should
// proceed. A result with Allow=false blocks the call.
type PreToolCallDecide func(ctx context.Context, hc *Context, call agtypes.ToolCall) (agtypes.HookResult, error)

func (PreToolCallDecide) isHook() {}

// PostToolCall is invoked after a tool call completes, receiving the result.
type PostToolCall func(ctx context.Context, hc *Context, result agtypes.ToolResult) error

func (PostToolCall) isHook() {}

// OnToolError is invoked when a tool fails. It receives the raised error and
// returns the error representation the model should see. Returning a nil
// replacement (with handled=false) signals that this hook did not handle the
// error, so the harness uses its default formatting; returning handled=true
// stops further hooks.
type OnToolError func(ctx context.Context, hc *Context, toolErr error) (replacement any, handled bool, err error)

func (OnToolError) isHook() {}

// OnInteraction is invoked when the agent needs user interaction. It receives
// an interaction spec and returns the user's responses. Returning handled=false
// signals that this hook did not handle the interaction.
type OnInteraction func(ctx context.Context, hc *Context, spec agtypes.AskQuestionInteractionSpec) (result agtypes.QuestionHookResult, handled bool, err error)

func (OnInteraction) isHook() {}

// OnCompaction is invoked when a context compaction event occurs. It is an
// observability point; data carries event details.
type OnCompaction func(ctx context.Context, hc *Context, data any) error

func (OnCompaction) isHook() {}
