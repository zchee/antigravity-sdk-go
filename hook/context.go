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

// Context is the base context for hooks to share state. Contexts form a chain
// (session → turn → operation); Get walks toward the root, while Set writes
// only to the local store.
//
// A Context is not safe for concurrent use; hooks within a single dispatch run
// sequentially.
type Context struct {
	parent *Context
	store  map[string]any
}

// newContext returns a Context with the given parent (nil for a session
// context).
func newContext(parent *Context) *Context {
	return &Context{parent: parent, store: make(map[string]any)}
}

// NewSessionContext returns a Context scoped to an entire session (no parent).
func NewSessionContext() *Context {
	return newContext(nil)
}

// NewTurnContext returns a Context scoped to a single turn, parented to the
// given session context.
func NewTurnContext(session *Context) *Context {
	return newContext(session)
}

// NewOperationContext returns a Context scoped to a specific operation (e.g. a
// tool call), parented to the given turn context.
func NewOperationContext(turn *Context) *Context {
	return newContext(turn)
}

// Parent returns the parent context, or nil for a session context.
func (c *Context) Parent() *Context { return c.parent }

// Get returns the value associated with key, searching this context and then
// its parents. The second result reports whether the key was found.
func (c *Context) Get(key string) (any, bool) {
	for cur := c; cur != nil; cur = cur.parent {
		if v, ok := cur.store[key]; ok {
			return v, true
		}
	}
	return nil, false
}

// GetOr returns the value associated with key, searching this context and its
// parents, or def if the key is not found.
func (c *Context) GetOr(key string, def any) any {
	if v, ok := c.Get(key); ok {
		return v
	}
	return def
}

// Set stores value under key in the local context only (not its parents).
func (c *Context) Set(key string, value any) {
	c.store[key] = value
}
