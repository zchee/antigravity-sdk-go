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
	"errors"
	"iter"
	"sync"
	"testing"

	"github.com/zchee/antigravity-sdk-go/agtypes"
)

// chunkSeq builds an iter.Seq2 stream that yields the given chunks, then a final
// error element if termErr is non-nil. It records how many times it is pulled
// so tests can assert the upstream is consumed exactly once across cursors.
func chunkSeq(chunks []agtypes.ChatChunk, termErr error, pulls *int) iter.Seq2[agtypes.ChatChunk, error] {
	return func(yield func(agtypes.ChatChunk, error) bool) {
		for _, c := range chunks {
			if pulls != nil {
				*pulls++
			}
			if !yield(c, nil) {
				return
			}
		}
		if termErr != nil {
			yield(nil, termErr)
		}
	}
}

// newTestResponse wraps a chunk stream with a nil-safe conversation reference
// for tests that do not touch StructuredOutput/UsageMetadata.
func newTestResponse(chunks []agtypes.ChatChunk, termErr error, pulls *int) *ChatResponse {
	return newChatResponse(chunkSeq(chunks, termErr, pulls), nil)
}

func sampleChunks() []agtypes.ChatChunk {
	return []agtypes.ChatChunk{
		agtypes.Thought{StepIndex: 0, Text: "thinking"},
		agtypes.Text{StepIndex: 0, Text: "Hello, "},
		agtypes.ToolCall{Name: "view_file", ID: "t1"},
		agtypes.Text{StepIndex: 1, Text: "world"},
	}
}

func TestChatResponseSingleCursor(t *testing.T) {
	r := newTestResponse(sampleChunks(), nil, nil)
	var got []agtypes.ChatChunk
	for chunk, err := range r.Chunks() {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got = append(got, chunk)
	}
	if len(got) != 4 {
		t.Fatalf("got %d chunks, want 4", len(got))
	}
}

func TestChatResponseViews(t *testing.T) {
	r := newTestResponse(sampleChunks(), nil, nil)
	// Text aggregation.
	text, err := r.Text()
	if err != nil {
		t.Fatal(err)
	}
	if text != "Hello, world" {
		t.Errorf("Text() = %q, want %q", text, "Hello, world")
	}

	// Thoughts and ToolCalls cursors are independent and replay from the buffer.
	var thoughts []string
	for th, err := range r.Thoughts() {
		if err != nil {
			t.Fatal(err)
		}
		thoughts = append(thoughts, th)
	}
	if len(thoughts) != 1 || thoughts[0] != "thinking" {
		t.Errorf("Thoughts() = %v, want [thinking]", thoughts)
	}

	var calls []agtypes.ToolCall
	for c, err := range r.ToolCalls() {
		if err != nil {
			t.Fatal(err)
		}
		calls = append(calls, c)
	}
	if len(calls) != 1 || calls[0].ID != "t1" {
		t.Errorf("ToolCalls() = %v, want one call t1", calls)
	}
}

func TestChatResponseUpstreamConsumedOnce(t *testing.T) {
	pulls := 0
	r := newTestResponse(sampleChunks(), nil, &pulls)
	// First cursor drains the upstream into the shared buffer.
	for range r.Chunks() {
	}
	afterFirst := pulls
	if afterFirst != 4 {
		t.Fatalf("first cursor pulled %d, want 4", afterFirst)
	}
	// Subsequent cursors must read from the buffer only — no further upstream
	// pulls. This is the actual "consumed once" guarantee.
	for range r.Thoughts() {
	}
	if _, err := r.Text(); err != nil {
		t.Fatal(err)
	}
	if pulls != afterFirst {
		t.Errorf("upstream pulled %d more times for later cursors, want 0 (buffer replay)", pulls-afterFirst)
	}
}

func TestChatResponseErrorFanOut(t *testing.T) {
	boom := errors.New("stream failed")
	r := newTestResponse(sampleChunks(), boom, nil)

	// First cursor drains and sees the terminal error.
	var n int
	var gotErr error
	for chunk, err := range r.Chunks() {
		if err != nil {
			gotErr = err
			break
		}
		_ = chunk
		n++
	}
	if n != 4 {
		t.Errorf("first cursor saw %d chunks before error, want 4", n)
	}
	if !errors.Is(gotErr, boom) {
		t.Errorf("first cursor error = %v, want %v", gotErr, boom)
	}

	// A late, independent cursor replays the buffered chunks and the same error.
	n = 0
	gotErr = nil
	for chunk, err := range r.Chunks() {
		if err != nil {
			gotErr = err
			break
		}
		_ = chunk
		n++
	}
	if n != 4 {
		t.Errorf("late cursor saw %d chunks, want 4 (replayed from buffer)", n)
	}
	if !errors.Is(gotErr, boom) {
		t.Errorf("late cursor error = %v, want %v (error fans out)", gotErr, boom)
	}
}

// TestChatResponseChunkWithError pins the calling convention: when the upstream
// yields a chunk and an error together, the chunk is discarded and the error is
// terminal (matching the upstream, where an error is a separate event that
// replaces the chunk rather than accompanying it).
func TestChatResponseChunkWithError(t *testing.T) {
	boom := errors.New("paired error")
	seq := func(yield func(agtypes.ChatChunk, error) bool) {
		yield(agtypes.Text{Text: "ok"}, nil)
		yield(agtypes.Text{Text: "discarded"}, boom) // chunk + error together
		yield(agtypes.Text{Text: "unreached"}, nil)
	}
	r := newChatResponse(seq, nil)
	var got []string
	var gotErr error
	for chunk, err := range r.Chunks() {
		if err != nil {
			gotErr = err
			break
		}
		got = append(got, chunk.(agtypes.Text).Text)
	}
	if len(got) != 1 || got[0] != "ok" {
		t.Errorf("chunks before error = %v, want [ok] (paired chunk discarded)", got)
	}
	if !errors.Is(gotErr, boom) {
		t.Errorf("error = %v, want %v", gotErr, boom)
	}
}

func TestChatResponseConcurrentCursors(t *testing.T) {
	// Many chunks so cursors genuinely interleave under -race.
	chunks := make([]agtypes.ChatChunk, 200)
	for i := range chunks {
		chunks[i] = agtypes.Text{StepIndex: i, Text: "x"}
	}
	r := newTestResponse(chunks, nil, nil)

	const cursors = 8
	var wg sync.WaitGroup
	counts := make([]int, cursors)
	for i := range cursors {
		wg.Go(func() {
			for _, err := range r.Chunks() {
				if err != nil {
					t.Errorf("cursor %d error: %v", i, err)
					return
				}
				counts[i]++
			}
		})
	}
	wg.Wait()
	for i, c := range counts {
		if c != len(chunks) {
			t.Errorf("cursor %d saw %d chunks, want %d", i, c, len(chunks))
		}
	}
}

func TestChatResponseEarlyBreakIndependence(t *testing.T) {
	r := newTestResponse(sampleChunks(), nil, nil)
	// Cursor A breaks after one chunk.
	for range r.Chunks() {
		break
	}
	// Cursor B still drains the whole stream.
	var n int
	for _, err := range r.Chunks() {
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != 4 {
		t.Errorf("second cursor saw %d chunks after first broke early, want 4", n)
	}
}

func TestChatResponseCloseIdempotent(t *testing.T) {
	r := newTestResponse(sampleChunks(), nil, nil)
	r.Close()
	r.Close() // must not panic
	// After Close, a cursor sees no chunks (stream stopped) and no error.
	for chunk, err := range r.Chunks() {
		t.Errorf("cursor after Close yielded (%v, %v), want nothing", chunk, err)
	}
}
