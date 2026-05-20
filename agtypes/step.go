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

// UsageMetadata is token usage metadata from the model API.
//
// Fields are nil when the data is not available (e.g. the step did not involve
// a model call). A pointer to 0 means the model explicitly reported zero tokens
// for that category.
type UsageMetadata struct {
	// PromptTokenCount is the number of tokens in the prompt.
	PromptTokenCount *int `json:"prompt_token_count,omitzero"`
	// CachedContentTokenCount is the number of tokens from cached content. These
	// are a subset of prompt tokens.
	CachedContentTokenCount *int `json:"cached_content_token_count,omitzero"`
	// CandidatesTokenCount is the number of tokens in the generated candidates
	// (excluding thinking).
	CandidatesTokenCount *int `json:"candidates_token_count,omitzero"`
	// ThoughtsTokenCount is the number of tokens used for thinking/reasoning.
	ThoughtsTokenCount *int `json:"thoughts_token_count,omitzero"`
	// TotalTokenCount is the sum of prompt + candidates + thinking tokens.
	TotalTokenCount *int `json:"total_token_count,omitzero"`
}

// Step represents one action in the agent trajectory.
//
// The upstream model permits extra fields (Pydantic extra="allow"); those are
// captured in Extra. The local connection layer extends Step with additional
// fields (see the connection/local package), which round-trip through Extra
// here when not promoted to named fields.
type Step struct {
	// ID is a unique string identifier for the step.
	ID string `json:"id,omitzero"`
	// StepIndex is the integer index of the step in the trajectory.
	StepIndex int `json:"step_index,omitzero"`
	// Type is the high-level type of the step.
	Type StepType `json:"type,omitzero"`
	// Source is the source that generated the step.
	Source StepSource `json:"source,omitzero"`
	// Target is the target interacting with this step.
	Target StepTarget `json:"target,omitzero"`
	// Status is the status of the step.
	Status StepStatus `json:"status,omitzero"`
	// Content is the output of the step.
	Content string `json:"content,omitzero"`
	// ContentDelta is text added since the last update for this step.
	ContentDelta string `json:"content_delta,omitzero"`
	// Thinking is the model reasoning/thinking for planner responses.
	Thinking string `json:"thinking,omitzero"`
	// ThinkingDelta is thinking added since the last update for this step.
	ThinkingDelta string `json:"thinking_delta,omitzero"`
	// ToolCalls are the tool calls associated with the step.
	ToolCalls []ToolCall `json:"tool_calls,omitzero"`
	// Error is a short error message if the step failed, or empty.
	Error string `json:"error,omitzero"`
	// IsCompleteResponse, when non-nil and true, marks this step as a completed
	// model response directed at the user, as distinct from a partial streaming
	// chunk. Multiple steps per turn may set this; consumers that want only the
	// last response should iterate fully.
	IsCompleteResponse *bool `json:"is_complete_response,omitzero"`
	// StructuredOutput is the structured output extracted from the finish step.
	// A nil value marshals to JSON null, matching the upstream Any | None = None
	// default (the field is intentionally not omitted).
	StructuredOutput any `json:"structured_output"`
	// UsageMetadata is the token usage for this step's model invocation, or nil
	// if this step did not involve a model call.
	UsageMetadata *UsageMetadata `json:"usage_metadata,omitzero"`
	// Extra captures any additional fields not mapped to a named field above,
	// mirroring the upstream Pydantic extra="allow" behavior.
	Extra map[string]any `json:",inline"`
}
