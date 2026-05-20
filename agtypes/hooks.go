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

// HookResult is the result of a decision hook execution.
type HookResult struct {
	// Allow reports whether execution should proceed. The zero value is false;
	// construct with Allow: true (or use AllowHookResult) to permit by default.
	Allow bool `json:"allow,omitzero"`
	// Message is an optional explanation or response message.
	Message string `json:"message,omitzero"`
}

// AllowHookResult returns a HookResult that permits execution. It mirrors the
// upstream default (allow=True), which a Go zero value does not provide.
func AllowHookResult() HookResult {
	return HookResult{Allow: true}
}

// QuestionResponse is an individual response for an AskQuestion entry.
type QuestionResponse struct {
	// SelectedOptionIDs are the option IDs selected.
	SelectedOptionIDs []string `json:"selected_option_ids,omitzero"`
	// FreeformResponse is a freeform text response.
	FreeformResponse string `json:"freeform_response,omitzero"`
	// Skipped, if true, marks the question as skipped.
	Skipped bool `json:"skipped,omitzero"`
}

// QuestionHookResult is the result of an interaction containing a list of
// responses.
type QuestionHookResult struct {
	// Responses are the per-question responses.
	Responses []QuestionResponse `json:"responses"`
	// Cancelled, if true, marks the interaction as cancelled.
	Cancelled bool `json:"cancelled,omitzero"`
}

// AskQuestionOption is an option for an AskQuestion entry. Treat as immutable.
type AskQuestionOption struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// AskQuestionEntry is a single question with predefined options. Treat as
// immutable.
type AskQuestionEntry struct {
	Question      string              `json:"question"`
	Options       []AskQuestionOption `json:"options"`
	IsMultiSelect bool                `json:"is_multi_select,omitzero"`
}

// AskQuestionInteractionSpec is the interaction spec for an ask_question
// dialog. Treat as immutable.
type AskQuestionInteractionSpec struct {
	Questions []AskQuestionEntry `json:"questions"`
}
