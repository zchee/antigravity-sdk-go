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
	"context"
	"iter"
	"slices"
	"sync"
	"time"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/connection"
)

// DefaultMaxHistorySize is the default cap on retained steps. When exceeded,
// the oldest steps are discarded. A conversation created with a size of 0
// retains all steps.
const DefaultMaxHistorySize = 10_000

// Conversation is a stateful session wrapping a single conversation with the
// agent. It accumulates step history, tracks turn-start and compaction indices,
// aggregates token usage, and offers convenience send/receive methods.
//
// Only one step iteration (ReceiveSteps, ReceiveChunks, or the stream behind
// Chat) may be active at a time; this invariant lets history accumulate exactly
// once per step. Send drains or waits out an in-progress turn before starting a
// new one.
//
// A Conversation is safe for concurrent method calls, but concurrent iteration
// is not: start a second turn only after the previous stream is drained.
type Conversation struct {
	conn           connection.Connection
	maxHistorySize int

	mu               sync.Mutex
	steps            []agtypes.Step
	turnStartIndices []int
	compactionIdx    []int
	cumulativeUsage  agtypes.UsageMetadata
	turnUsage        *agtypes.UsageMetadata

	// iterating guards the single-active-iterator invariant.
	iterating bool
}

// NewConversation wraps an established connection. maxHistorySize caps retained
// steps (0 disables the cap).
func NewConversation(conn connection.Connection, maxHistorySize int) *Conversation {
	return &Conversation{
		conn:            conn,
		maxHistorySize:  maxHistorySize,
		cumulativeUsage: zeroUsage(),
	}
}

// CreateConversation starts a backend via strategy and returns a Conversation
// over the resulting connection. The caller owns teardown and must call
// Disconnect (or the strategy's Close) when done.
//
// It mirrors the upstream Conversation.create context manager as an explicit
// start/return: Start the strategy, Connect, and wrap. On any failure the
// strategy is closed before returning.
func CreateConversation(ctx context.Context, strategy connection.ConnectionStrategy) (*Conversation, error) {
	if err := strategy.Start(ctx); err != nil {
		return nil, err
	}
	conn, err := strategy.Connect()
	if err != nil {
		_ = strategy.Close(ctx)
		return nil, err
	}
	return NewConversation(conn, DefaultMaxHistorySize), nil
}

// Send sends a message to the agent. If a turn is still in progress it is first
// drained into history (or, if another goroutine already holds the iterator,
// Send waits for the connection to go idle). It then records the turn start and
// forwards the prompt.
func (c *Conversation) Send(ctx context.Context, prompt agtypes.Content) error {
	if !c.conn.IsIdle() {
		if c.tryBeginIterate() {
			// We acquired the iterator: drain the stale turn ourselves.
			c.drainLockedIterator(ctx)
			c.endIterate()
		} else {
			// Another goroutine is iterating and recording steps; just wait.
			if err := c.conn.WaitForIdle(ctx); err != nil {
				return err
			}
		}
	}
	c.mu.Lock()
	c.turnStartIndices = append(c.turnStartIndices, len(c.steps))
	c.turnUsage = nil
	c.mu.Unlock()
	return c.conn.Send(ctx, prompt)
}

// drainLockedIterator consumes the connection's step stream into history. The
// caller must hold the iterator (see tryBeginIterate).
func (c *Conversation) drainLockedIterator(ctx context.Context) {
	for _, err := range c.conn.ReceiveSteps(ctx) {
		if err != nil {
			return
		}
	}
}

// ReceiveSteps returns an iterator over steps as they complete, recording each
// into history (with compaction-index and usage tracking) before yielding it. A
// terminal stream error is reported as a final (zero, err) element.
//
// Only one iteration may be active at a time; if another is in progress this
// returns an iterator that immediately yields ErrIterating.
func (c *Conversation) ReceiveSteps(ctx context.Context) iter.Seq2[agtypes.Step, error] {
	return func(yield func(agtypes.Step, error) bool) {
		if !c.tryBeginIterate() {
			yield(agtypes.Step{}, ErrIterating)
			return
		}
		defer c.endIterate()
		for step, err := range c.conn.ReceiveSteps(ctx) {
			if err != nil {
				yield(agtypes.Step{}, err)
				return
			}
			c.recordStep(step)
			if !yield(step, nil) {
				return
			}
		}
	}
}

// recordStep appends a step to history and updates compaction indices, usage,
// and the history cap. It is safe for concurrent use.
func (c *Conversation) recordStep(step agtypes.Step) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.steps = append(c.steps, step)
	if step.Type == agtypes.StepTypeCompaction {
		c.compactionIdx = append(c.compactionIdx, len(c.steps)-1)
	}
	if step.UsageMetadata != nil {
		c.accumulateUsageLocked(*step.UsageMetadata)
	}
	c.enforceMaxHistoryLocked()
}

// ReceiveChunks returns an iterator over real-time semantic chunks (Thought,
// Text, ToolCall) for the current turn, derived from the step stream. Tool
// calls are de-duplicated by ID across the steps of this single iteration;
// calls without an ID are always yielded. Dedup state is per-call: a new
// ReceiveChunks (i.e. a new turn) starts with an empty seen-set, so an ID that
// recurs in a later turn is yielded again. A terminal error is reported as a
// final (nil, err) element.
func (c *Conversation) ReceiveChunks(ctx context.Context) iter.Seq2[agtypes.ChatChunk, error] {
	return func(yield func(agtypes.ChatChunk, error) bool) {
		seen := make(map[string]struct{})
		for step, err := range c.ReceiveSteps(ctx) {
			if err != nil {
				yield(nil, err)
				return
			}
			isModel := step.Source == agtypes.StepSourceModel
			isTargetUser := step.Target == agtypes.StepTargetUser
			if isModel && isTargetUser {
				if step.ThinkingDelta != "" {
					if !yield(agtypes.Thought{StepIndex: step.StepIndex, Text: step.ThinkingDelta}, nil) {
						return
					}
				}
				if step.ContentDelta != "" {
					if !yield(agtypes.Text{StepIndex: step.StepIndex, Text: step.ContentDelta}, nil) {
						return
					}
				}
			}
			for _, call := range step.ToolCalls {
				if call.ID != "" {
					if _, ok := seen[call.ID]; ok {
						continue
					}
					seen[call.ID] = struct{}{}
				}
				if !yield(call, nil) {
					return
				}
			}
		}
	}
}

// Chat sends a prompt and immediately returns a streaming ChatResponse over the
// turn's chunks. It does not block on the stream; consume the ChatResponse to
// drive it.
func (c *Conversation) Chat(ctx context.Context, prompt agtypes.Content) (*ChatResponse, error) {
	if err := c.Send(ctx, prompt); err != nil {
		return nil, err
	}
	return newChatResponse(c.ReceiveChunks(ctx), c), nil
}

// LastStructuredOutput returns the structured output from the most recent
// finish step, or nil if none.
func (c *Conversation) LastStructuredOutput() any {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, v := range slices.Backward(c.steps) {
		if v.Type == agtypes.StepTypeFinish {
			return v.StructuredOutput
		}
	}
	return nil
}

// History returns a copy of all steps received across all turns: the full,
// uncompacted transcript. Use CompactionIndices to locate context compactions.
func (c *Conversation) History() []agtypes.Step {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.steps)
}

// LastResponse returns the content of the most recent completed model response.
func (c *Conversation) LastResponse() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range slices.Backward(c.steps) {
		if s.IsCompleteResponse != nil && *s.IsCompleteResponse {
			return s.Content
		}
	}
	return ""
}

// TurnCount returns the number of Send calls made on this conversation.
func (c *Conversation) TurnCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.turnStartIndices)
}

// CompactionIndices returns a copy of the step indices where the model's
// context was compacted.
func (c *Conversation) CompactionIndices() []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.compactionIdx)
}

// ClearHistory discards accumulated history, turn indices, compaction indices,
// and usage totals. The conversation remains active.
func (c *Conversation) ClearHistory() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.steps = nil
	c.turnStartIndices = nil
	c.compactionIdx = nil
	c.cumulativeUsage = zeroUsage()
	c.turnUsage = nil
}

// Connection returns the underlying transport. Intended for advanced use;
// bypassing the Conversation skips history tracking.
func (c *Conversation) Connection() connection.Connection { return c.conn }

// IsIdle reports whether the conversation is idle and ready for input.
func (c *Conversation) IsIdle() bool { return c.conn.IsIdle() }

// ConversationID returns the conversation identifier, if any.
func (c *Conversation) ConversationID() string { return c.conn.ConversationID() }

// TotalUsage returns cumulative token usage across all turns in this session.
func (c *Conversation) TotalUsage() agtypes.UsageMetadata {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cumulativeUsage
}

// LastTurnUsage returns token usage accumulated during the most recent turn.
// The boolean is false when no step in the latest turn reported usage.
func (c *Conversation) LastTurnUsage() (agtypes.UsageMetadata, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.turnUsage == nil {
		return agtypes.UsageMetadata{}, false
	}
	return *c.turnUsage, true
}

// Cancel cancels the current turn in progress.
func (c *Conversation) Cancel(ctx context.Context) error { return c.conn.Cancel(ctx) }

// Delete deletes this conversation and all backend state.
func (c *Conversation) Delete(ctx context.Context) error { return c.conn.Delete(ctx) }

// SignalIdle signals that the conversation is ready to receive input.
func (c *Conversation) SignalIdle(ctx context.Context) error { return c.conn.SignalIdle(ctx) }

// WaitForIdle blocks until the conversation is idle or ctx is cancelled.
func (c *Conversation) WaitForIdle(ctx context.Context) error { return c.conn.WaitForIdle(ctx) }

// WaitForWakeup blocks until the conversation wakes up or timeout elapses,
// returning true on wakeup and false on timeout.
func (c *Conversation) WaitForWakeup(ctx context.Context, timeout time.Duration) (bool, error) {
	return c.conn.WaitForWakeup(ctx, timeout)
}

// Disconnect closes the connection transport and releases resources.
func (c *Conversation) Disconnect(ctx context.Context) error { return c.conn.Disconnect(ctx) }

// tryBeginIterate acquires the single-iterator guard, returning false if an
// iteration is already active.
func (c *Conversation) tryBeginIterate() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.iterating {
		return false
	}
	c.iterating = true
	return true
}

// endIterate releases the single-iterator guard.
func (c *Conversation) endIterate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.iterating = false
}

// enforceMaxHistoryLocked trims history to maxHistorySize, adjusting indices.
// The caller must hold c.mu.
func (c *Conversation) enforceMaxHistoryLocked() {
	if c.maxHistorySize <= 0 || len(c.steps) <= c.maxHistorySize {
		return
	}
	overflow := len(c.steps) - c.maxHistorySize
	c.steps = slices.Delete(c.steps, 0, overflow)
	c.turnStartIndices = shiftIndices(c.turnStartIndices, overflow)
	c.compactionIdx = shiftIndices(c.compactionIdx, overflow)
}

// shiftIndices subtracts overflow from each index, dropping those that fall
// before zero.
func shiftIndices(idx []int, overflow int) []int {
	out := idx[:0]
	for _, i := range idx {
		if i >= overflow {
			out = append(out, i-overflow)
		}
	}
	return out
}

// accumulateUsageLocked adds per-step usage to the cumulative and per-turn
// totals. The caller must hold c.mu.
func (c *Conversation) accumulateUsageLocked(u agtypes.UsageMetadata) {
	addUsage(&c.cumulativeUsage, u)
	if c.turnUsage == nil {
		z := zeroUsage()
		c.turnUsage = &z
	}
	addUsage(c.turnUsage, u)
}

// ErrIterating reports an attempt to start a second concurrent step iteration
// on a Conversation.
var ErrIterating = errorString("antigravity: a step iteration is already in progress")

// errorString is a lightweight constant error type.
type errorString string

func (e errorString) Error() string { return string(e) }

// zeroUsage returns a UsageMetadata with every counter explicitly zero. Each
// field gets its own pointer so callers may mutate them independently.
func zeroUsage() agtypes.UsageMetadata {
	return agtypes.UsageMetadata{
		PromptTokenCount:        new(0),
		CachedContentTokenCount: new(0),
		CandidatesTokenCount:    new(0),
		ThoughtsTokenCount:      new(0),
		TotalTokenCount:         new(0),
	}
}

// addUsage adds the counts in src into dst, treating nil counts as zero.
func addUsage(dst *agtypes.UsageMetadata, src agtypes.UsageMetadata) {
	dst.PromptTokenCount = new(deref(dst.PromptTokenCount) + deref(src.PromptTokenCount))
	dst.CachedContentTokenCount = new(deref(dst.CachedContentTokenCount) + deref(src.CachedContentTokenCount))
	dst.CandidatesTokenCount = new(deref(dst.CandidatesTokenCount) + deref(src.CandidatesTokenCount))
	dst.ThoughtsTokenCount = new(deref(dst.ThoughtsTokenCount) + deref(src.ThoughtsTokenCount))
	dst.TotalTokenCount = new(deref(dst.TotalTokenCount) + deref(src.TotalTokenCount))
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
