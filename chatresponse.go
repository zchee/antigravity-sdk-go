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

package antigravity

import (
	"iter"
	"strings"
	"sync"

	"github.com/zchee/antigravity-sdk-go/agtypes"
)

// ChatResponse is the streaming response from a chat turn. It wraps the turn's
// chunk stream with lazy buffering and exposes several views over it.
//
// Every iterator method (Chunks, Thoughts, ToolCalls, TextDeltas) returns an
// independent cursor positioned at the start of the turn. Cursors may be
// consumed sequentially or concurrently from multiple goroutines; pulls from
// the underlying stream are serialized, and each chunk pulled is appended to a
// shared buffer that every cursor reads. If the underlying stream fails, the
// error is stored and reported to every cursor that reaches the live edge after
// the failure.
//
// A ChatResponse is returned by chat methods, not constructed directly. Call
// Close when done to release the underlying stream's resources; Resolve, Text,
// and StructuredOutput call Close implicitly once the stream is drained.
type ChatResponse struct {
	conv *Conversation

	mu      sync.Mutex
	buf     []agtypes.ChatChunk
	done    bool
	err     error
	next    func() (agtypes.ChatChunk, error, bool)
	stop    func()
	stopped bool
}

// newChatResponse wraps a chunk stream in a ChatResponse backed by the given
// conversation (used by StructuredOutput and UsageMetadata).
func newChatResponse(stream iter.Seq2[agtypes.ChatChunk, error], conv *Conversation) *ChatResponse {
	next, stop := iter.Pull2(stream)
	return &ChatResponse{conv: conv, next: next, stop: stop}
}

// at returns the chunk at position pos, pulling one more from the upstream
// stream if pos is at the live edge. The boolean reports whether a chunk is
// available; when false, err carries any terminal stream error (nil on clean
// end). The pull is performed under the lock so concurrent cursors serialize on
// the single upstream stream.
func (r *ChatResponse) at(pos int) (agtypes.ChatChunk, error, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pos < len(r.buf) {
		return r.buf[pos], nil, true
	}
	if r.done {
		return nil, r.err, false
	}
	chunk, err, ok := r.next()
	if !ok {
		// Upstream ended; record terminal state (error sticks for late cursors).
		r.done = true
		r.err = err
		r.stopLocked()
		return nil, err, false
	}
	if err != nil {
		// iter.Pull2 reports an error alongside ok=true for the failing element;
		// surface it and mark the stream done so later cursors also see it.
		r.done = true
		r.err = err
		r.stopLocked()
		return nil, err, false
	}
	r.buf = append(r.buf, chunk)
	return chunk, nil, true
}

// chunks returns an independent cursor over all chunks, yielding any terminal
// error as the final (zero, err) pair.
func (r *ChatResponse) chunks() iter.Seq2[agtypes.ChatChunk, error] {
	return func(yield func(agtypes.ChatChunk, error) bool) {
		for pos := 0; ; pos++ {
			chunk, err, ok := r.at(pos)
			if !ok {
				if err != nil {
					yield(nil, err)
				}
				return
			}
			if !yield(chunk, nil) {
				return
			}
		}
	}
}

// Chunks returns an independent cursor over the raw chunk stream (Thought,
// Text, and ToolCall values, interleaved as produced). A terminal stream error
// is reported as a final (nil, err) element.
func (r *ChatResponse) Chunks() iter.Seq2[agtypes.ChatChunk, error] {
	return r.chunks()
}

// Thoughts returns an independent cursor over the model's reasoning deltas as
// strings. A terminal stream error is reported as a final ("", err) element.
func (r *ChatResponse) Thoughts() iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		for chunk, err := range r.chunks() {
			if err != nil {
				yield("", err)
				return
			}
			if t, ok := chunk.(agtypes.Thought); ok {
				if !yield(t.Text, nil) {
					return
				}
			}
		}
	}
}

// TextDeltas returns an independent cursor over the model's response text
// deltas as strings. A terminal stream error is reported as a final ("", err)
// element.
func (r *ChatResponse) TextDeltas() iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		for chunk, err := range r.chunks() {
			if err != nil {
				yield("", err)
				return
			}
			if t, ok := chunk.(agtypes.Text); ok {
				if !yield(t.Text, nil) {
					return
				}
			}
		}
	}
}

// ToolCalls returns an independent cursor over the tool calls dispatched during
// the turn. A terminal stream error is reported as a final (zero, err) element.
func (r *ChatResponse) ToolCalls() iter.Seq2[agtypes.ToolCall, error] {
	return func(yield func(agtypes.ToolCall, error) bool) {
		for chunk, err := range r.chunks() {
			if err != nil {
				yield(agtypes.ToolCall{}, err)
				return
			}
			if c, ok := chunk.(agtypes.ToolCall); ok {
				if !yield(c, nil) {
					return
				}
			}
		}
	}
}

// Resolve drains the entire stream and returns all chunks in order. It returns
// the terminal stream error, if any. After Resolve the stream is closed.
func (r *ChatResponse) Resolve() ([]agtypes.ChatChunk, error) {
	var out []agtypes.ChatChunk
	for chunk, err := range r.chunks() {
		if err != nil {
			return out, err
		}
		out = append(out, chunk)
	}
	return out, nil
}

// Text drains the stream and returns the fully aggregated response text (the
// concatenation of all Text deltas). It returns the terminal stream error, if
// any.
func (r *ChatResponse) Text() (string, error) {
	var b strings.Builder
	for chunk, err := range r.chunks() {
		if err != nil {
			return b.String(), err
		}
		if t, ok := chunk.(agtypes.Text); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String(), nil
}

// StructuredOutput drains the stream if necessary and returns the structured
// output extracted from the turn's finish step, or nil if none. It returns the
// terminal stream error, if any.
func (r *ChatResponse) StructuredOutput() (any, error) {
	r.mu.Lock()
	done := r.done
	r.mu.Unlock()
	if !done {
		if _, err := r.Resolve(); err != nil {
			return nil, err
		}
	}
	return r.conv.LastStructuredOutput(), nil
}

// UsageMetadata returns the token usage accumulated during this turn. It is a
// live read of the conversation and is meaningful once the stream completes.
func (r *ChatResponse) UsageMetadata() (agtypes.UsageMetadata, bool) {
	return r.conv.LastTurnUsage()
}

// Close releases the resources held by the underlying stream. It is safe to
// call multiple times. Resolve, Text, and StructuredOutput close the stream
// implicitly once it is drained.
func (r *ChatResponse) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked()
}

// stopLocked calls the iter.Pull2 stop function once. The caller must hold r.mu.
func (r *ChatResponse) stopLocked() {
	if !r.stopped {
		r.stopped = true
		r.stop()
	}
}
