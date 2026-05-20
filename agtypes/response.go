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

// StreamChunk is the interface implemented by every real-time semantic chunk
// yielded during agent chat streaming (Thought, Text). Implementations are
// closed to this package via the unexported marker method. Treat values as
// immutable.
type StreamChunk interface {
	isStreamChunk()
	// Index returns the step index this chunk belongs to.
	Index() int
}

// Thought is a delta chunk representing a piece of the model's internal
// reasoning/thinking.
type Thought struct {
	// StepIndex is the step index this chunk belongs to.
	StepIndex int `json:"step_index"`
	// Text is the incremental thought string delta.
	Text string `json:"text"`
	// Signature is an optional opaque signature for the thought.
	Signature []byte `json:"signature,omitzero"`
}

func (t Thought) isStreamChunk() {}

// Index returns the step index this chunk belongs to.
func (t Thought) Index() int { return t.StepIndex }

// Text is a delta chunk representing a piece of the model's text output.
type Text struct {
	// StepIndex is the step index this chunk belongs to.
	StepIndex int `json:"step_index"`
	// Text is the incremental response string delta.
	Text string `json:"text"`
}

func (t Text) isStreamChunk() {}

// Index returns the step index this chunk belongs to.
func (t Text) Index() int { return t.StepIndex }

// ChatChunk is a single real-time event in a chat stream: either a StreamChunk
// (Thought or Text) or a ToolCall.
//
// Upstream models this as the union StreamChunk | ToolCall. ToolCall is
// identity-keyed (by ID) and carries no step index, so it is not a StreamChunk;
// Go cannot express a closed union over an interface and a struct, so ChatChunk
// is an alias for any. A consumer type-switches on StreamChunk vs ToolCall. The
// streaming ChatResponse that buffers these lives in the root antigravity
// package, which holds the back-reference to the Conversation it reads from.
type ChatChunk = any
