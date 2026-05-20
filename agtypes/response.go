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

// ChatResponse is the turn response from an Agent chat call: a stream of
// semantic chunks with lazy buffering, exposing independent cursors over a
// shared buffer.
//
// NOTE: this is a declaration-only placeholder for Phase 2 of the port. The
// multiple-independent-cursor buffering, error fan-out, and the Chunks /
// Thoughts / ToolCalls / Text / Resolve / StructuredOutput / UsageMetadata
// surface are implemented in Phase 6, where the concurrency contract is
// designed in isolation. The fields and methods here are intentionally minimal
// so that downstream packages can name the type without depending on the final
// streaming implementation.
type ChatResponse struct {
	// _ prevents a meaningful zero value: a ChatResponse is constructed by the
	// SDK, not by callers. Real fields (shared buffer, cursors, error state) are
	// added in Phase 6.
	_ noCopy
}

// noCopy is a zero-width marker preventing useful zero-value construction and
// signaling that the type must not be copied once the Phase 6 implementation
// adds synchronization. It has no behavior on its own.
type noCopy struct{}
