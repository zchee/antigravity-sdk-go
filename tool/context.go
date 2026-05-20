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

// Package tool provides the in-process tool registry and executor for the
// Antigravity SDK, along with the conversation-aware ToolContext injected into
// tools that need it.
//
// Upstream injects a ToolContext into any tool that declares a
// ToolContext-typed parameter, using runtime signature inspection. Go has no
// equivalent kwarg binding, so the Go port threads the ToolContext through the
// standard context.Context: the Runner attaches the active ToolContext to the
// context.Context it passes to each Tool, and a tool retrieves it with
// FromContext. Tools that do not need it simply ignore the context value.
package tool

import (
	"context"
	"sync"
)

// conn is the narrow slice of a connection that ToolContext depends on. The
// full connection.Connection (defined in a later phase) structurally satisfies
// it. Declaring the dependency locally keeps this package dependent only on
// agtypes and the standard library, and follows the accept-interfaces idiom.
type conn interface {
	// ConversationID returns the conversation identifier.
	ConversationID() string
	// IsIdle reports whether the connection is idle.
	IsIdle() bool
	// SendTriggerNotification injects a trigger-style notification into the
	// conversation.
	SendTriggerNotification(ctx context.Context, message string) error
}

// ToolContext is the conversation-aware handle injected into tools that need
// it. It wraps a connection and exposes a curated set of conversation
// capabilities plus a per-conversation key-value store. One ToolContext is
// created per session and shared across all tools.
//
// The state store is safe for concurrent use because tools may execute
// concurrently (see Runner.ProcessToolCalls).
type ToolContext struct {
	connection conn

	mu    sync.RWMutex
	state map[string]any
}

// NewToolContext returns a ToolContext wrapping the given connection.
func NewToolContext(c conn) *ToolContext {
	return &ToolContext{connection: c, state: make(map[string]any)}
}

// ConversationID returns the conversation identifier.
func (t *ToolContext) ConversationID() string { return t.connection.ConversationID() }

// IsIdle reports whether the connection is idle.
func (t *ToolContext) IsIdle() bool { return t.connection.IsIdle() }

// Send injects a trigger-style notification into the conversation, allowing a
// tool to asynchronously push a follow-up message.
func (t *ToolContext) Send(ctx context.Context, message string) error {
	return t.connection.SendTriggerNotification(ctx, message)
}

// GetState returns the value stored under key, or (nil, false) if absent.
func (t *ToolContext) GetState(key string) (any, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	v, ok := t.state[key]
	return v, ok
}

// SetState stores value under key in the per-conversation state store.
func (t *ToolContext) SetState(key string, value any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state[key] = value
}

// ctxKey is the unexported context key under which the ToolContext is stored.
type ctxKey struct{}

// WithToolContext returns a copy of ctx carrying tc, retrievable via
// FromContext. The Runner uses this to inject the active ToolContext.
func WithToolContext(ctx context.Context, tc *ToolContext) context.Context {
	return context.WithValue(ctx, ctxKey{}, tc)
}

// FromContext returns the ToolContext attached to ctx, or (nil, false) if none
// is present.
func FromContext(ctx context.Context) (*ToolContext, bool) {
	tc, ok := ctx.Value(ctxKey{}).(*ToolContext)
	return tc, ok
}
