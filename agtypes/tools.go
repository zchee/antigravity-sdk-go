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

package agtypes

// ToolCall is a tool call to inject into the conversation.
type ToolCall struct {
	// Name is the tool identifier. Use a BuiltinTools value for
	// connection-provided tools, or an arbitrary string for custom host-side
	// tools. The field is typed as string to admit both.
	Name string `json:"name"`
	// Args are the keyword arguments for the tool, as a JSON-serializable map.
	// A nil map marshals to an empty JSON object, matching the upstream
	// default_factory=dict behavior.
	Args map[string]any `json:"args"`
	// ID is an optional unique identifier for the call, often assigned by the
	// backend.
	ID string `json:"id,omitzero"`
	// CanonicalPath is an optional normalized filesystem path for file-related
	// tools. It is populated by the connection layer to enable platform-agnostic
	// L2 policies.
	CanonicalPath string `json:"canonical_path,omitzero"`
}

// ToolResult is the result of a single tool execution.
type ToolResult struct {
	// Name is the name of the tool that was executed. A BuiltinTools value for
	// connection-provided tools, or a string for custom host-side tools.
	Name string `json:"name"`
	// ID is an optional identifier correlating this result with a ToolCall.ID.
	ID string `json:"id,omitzero"`
	// Result is the tool's return value. It can be any JSON-serializable value.
	// A nil result marshals to JSON null, matching the upstream result=None
	// default (the field is intentionally not omitted).
	Result any `json:"result"`
	// Error is an error message if execution failed, or empty on success.
	Error string `json:"error,omitzero"`
	// Exception is the original error if execution failed. It is never
	// serialized (mirrors the upstream exclude=True field).
	Exception error `json:"-"`
}
