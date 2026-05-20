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

package tool

import (
	"context"
	"fmt"
	"maps"
	"sync"

	"github.com/zchee/antigravity-sdk-go/agtypes"
)

// Tool is an in-process tool: a function invoked by name with a JSON-decoded
// argument map, returning a JSON-serializable result or an error.
//
// This is the Go calling convention replacing the upstream PythonTool
// (Callable[..., Any]) with its reflection-based **kwargs binding. Arguments
// arrive as a single map rather than bound parameters; a tool needing the
// conversation-aware ToolContext retrieves it from ctx via FromContext.
type Tool func(ctx context.Context, args map[string]any) (any, error)

// ToolWithSchema pairs a Tool with an explicit JSON Schema describing its input
// arguments. Connections use the schema when advertising the tool to the model.
type ToolWithSchema struct {
	// Fn is the tool implementation.
	Fn Tool
	// InputSchema is the JSON Schema for the tool's arguments.
	InputSchema map[string]any
}

// registered is the internal record for one tool: its callable and optional
// schema.
type registered struct {
	fn     Tool
	schema map[string]any
}

// Runner is a registry and executor for in-process tools. Tools are registered
// by name and executed on demand. It is safe for concurrent use.
type Runner struct {
	mu    sync.RWMutex
	tools map[string]registered
	tc    *ToolContext
}

// NewRunner returns an empty Runner.
func NewRunner() *Runner {
	return &Runner{tools: make(map[string]registered)}
}

// SetContext sets the ToolContext injected into the context.Context passed to
// each tool at execution time.
func (r *Runner) SetContext(tc *ToolContext) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tc = tc
}

// ErrToolAlreadyRegistered reports an attempt to register a name already in use.
type ErrToolAlreadyRegistered struct{ Name string }

func (e *ErrToolAlreadyRegistered) Error() string {
	return fmt.Sprintf("tool: %q is already registered", e.Name)
}

// ErrToolNotRegistered reports a reference to an unregistered tool name.
type ErrToolNotRegistered struct{ Name string }

func (e *ErrToolNotRegistered) Error() string {
	return fmt.Sprintf("tool: %q is not registered", e.Name)
}

// Register adds a Tool under name. It returns an *ErrToolAlreadyRegistered if
// the name is already in use.
func (r *Runner) Register(name string, fn Tool) error {
	return r.register(name, registered{fn: fn})
}

// RegisterWithSchema adds a ToolWithSchema under name, retaining its input
// schema. It returns an *ErrToolAlreadyRegistered if the name is already in use.
func (r *Runner) RegisterWithSchema(name string, t ToolWithSchema) error {
	return r.register(name, registered{fn: t.Fn, schema: t.InputSchema})
}

func (r *Runner) register(name string, rec registered) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; ok {
		return &ErrToolAlreadyRegistered{Name: name}
	}
	r.tools[name] = rec
	return nil
}

// Unregister removes the tool registered under name. It returns an
// *ErrToolNotRegistered if no such tool exists.
func (r *Runner) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; !ok {
		return &ErrToolNotRegistered{Name: name}
	}
	delete(r.tools, name)
	return nil
}

// Names returns the names of all registered tools in unspecified order.
func (r *Runner) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// Schema returns the input JSON Schema registered for name, or (nil, false) if
// the tool has no schema or is not registered.
func (r *Runner) Schema(name string) (map[string]any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.tools[name]
	if !ok || rec.schema == nil {
		return nil, false
	}
	return maps.Clone(rec.schema), true
}

// Execute runs the tool registered under name with args, injecting the active
// ToolContext into the context. It returns an *ErrToolNotRegistered if the tool
// is not registered, or whatever error the tool returns.
func (r *Runner) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	r.mu.RLock()
	rec, ok := r.tools[name]
	tc := r.tc
	r.mu.RUnlock()
	if !ok {
		return nil, &ErrToolNotRegistered{Name: name}
	}
	if tc != nil {
		ctx = WithToolContext(ctx, tc)
	}
	return rec.fn(ctx, args)
}

// ProcessToolCalls executes a batch of tool calls concurrently and returns one
// ToolResult per input call, in the same order. Unknown tools and tool errors
// produce a ToolResult with a populated Error rather than failing the batch.
//
// Tools execute concurrently; callers must not depend on sequential
// side-effect ordering.
func (r *Runner) ProcessToolCalls(ctx context.Context, calls []agtypes.ToolCall) []agtypes.ToolResult {
	results := make([]agtypes.ToolResult, len(calls))
	var wg sync.WaitGroup
	for i, tc := range calls {
		wg.Go(func() {
			results[i] = r.executeOne(ctx, tc)
		})
	}
	wg.Wait()
	return results
}

// executeOne runs a single tool call, converting an unknown tool or a tool
// error into a ToolResult rather than propagating it.
func (r *Runner) executeOne(ctx context.Context, tc agtypes.ToolCall) agtypes.ToolResult {
	result, err := r.Execute(ctx, tc.Name, tc.Args)
	if err != nil {
		return agtypes.ToolResult{Name: tc.Name, ID: tc.ID, Error: err.Error(), Exception: err}
	}
	return agtypes.ToolResult{Name: tc.Name, ID: tc.ID, Result: result}
}
