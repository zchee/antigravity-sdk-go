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
	"testing"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/connection"
)

// blockingConn is a connection whose ReceiveSteps yields one step and then
// blocks until release is closed (or ctx is cancelled), letting a test hold the
// conversation's iterator open.
type blockingConn struct {
	connection.BaseConnection
	release chan struct{}
}

func (b *blockingConn) Send(context.Context, agtypes.Content) error { return nil }
func (b *blockingConn) SendTriggerNotification(context.Context, string) error {
	return nil
}

func (b *blockingConn) ReceiveSteps(ctx context.Context) iter.Seq2[agtypes.Step, error] {
	return func(yield func(agtypes.Step, error) bool) {
		if !yield(agtypes.Step{ID: "first"}, nil) {
			return
		}
		select {
		case <-b.release:
		case <-ctx.Done():
		}
	}
}

// pull2Steps adapts a step iterator to an explicit next/stop pair for tests.
func pull2Steps(seq iter.Seq2[agtypes.Step, error]) (func() (agtypes.Step, error, bool), func()) {
	return iter.Pull2(seq)
}

func TestConversationReceiveStepsRecordsHistory(t *testing.T) {
	steps := []agtypes.Step{
		{ID: "s0", StepIndex: 0, Type: agtypes.StepTypeTextResponse},
		{ID: "s1", StepIndex: 1, Type: agtypes.StepTypeCompaction},
		{ID: "s2", StepIndex: 2, Type: agtypes.StepTypeFinish, StructuredOutput: map[string]any{"k": "v"}},
	}
	conv := NewConversation(&connection.FakeConnection{Steps: steps}, DefaultMaxHistorySize)

	var n int
	for _, err := range conv.ReceiveSteps(t.Context()) {
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != 3 {
		t.Fatalf("received %d steps, want 3", n)
	}
	if got := conv.History(); len(got) != 3 {
		t.Errorf("History len = %d, want 3", len(got))
	}
	if got := conv.CompactionIndices(); len(got) != 1 || got[0] != 1 {
		t.Errorf("CompactionIndices = %v, want [1]", got)
	}
	if got := conv.LastStructuredOutput(); got == nil {
		t.Error("LastStructuredOutput = nil, want the finish step output")
	}
}

func TestConversationHistoryIsCopy(t *testing.T) {
	conv := NewConversation(&connection.FakeConnection{Steps: []agtypes.Step{{ID: "s0"}}}, 0)
	for range conv.ReceiveSteps(t.Context()) {
	}
	h := conv.History()
	h[0].ID = "mutated"
	if conv.History()[0].ID != "s0" {
		t.Error("History did not return a defensive copy")
	}
}

func TestConversationLastResponse(t *testing.T) {
	steps := []agtypes.Step{
		{Content: "first", IsCompleteResponse: new(true)},
		{Content: "partial", IsCompleteResponse: nil},
		{Content: "final", IsCompleteResponse: new(true)},
	}
	conv := NewConversation(&connection.FakeConnection{Steps: steps}, 0)
	for range conv.ReceiveSteps(t.Context()) {
	}
	if got := conv.LastResponse(); got != "final" {
		t.Errorf("LastResponse = %q, want final", got)
	}
}

func TestConversationMaxHistoryTrim(t *testing.T) {
	steps := make([]agtypes.Step, 5)
	for i := range steps {
		steps[i] = agtypes.Step{StepIndex: i}
	}
	// Mark step 1 as a compaction so we can verify index adjustment after trim.
	steps[1].Type = agtypes.StepTypeCompaction
	conv := NewConversation(&connection.FakeConnection{Steps: steps}, 3)
	for range conv.ReceiveSteps(t.Context()) {
	}
	// Only the last 3 steps are retained.
	if got := conv.History(); len(got) != 3 {
		t.Fatalf("History len = %d, want 3 (trimmed)", len(got))
	}
	// The compaction at original index 1 fell off (overflow=2), so no indices remain.
	if got := conv.CompactionIndices(); len(got) != 0 {
		t.Errorf("CompactionIndices = %v, want [] (compaction trimmed away)", got)
	}
}

func TestConversationUsageAccumulation(t *testing.T) {
	steps := []agtypes.Step{
		{UsageMetadata: &agtypes.UsageMetadata{PromptTokenCount: new(10), TotalTokenCount: new(15)}},
		{UsageMetadata: &agtypes.UsageMetadata{PromptTokenCount: new(5), TotalTokenCount: new(8)}},
		{UsageMetadata: nil},
	}
	conv := NewConversation(&connection.FakeConnection{Steps: steps}, 0)
	if err := conv.Send(t.Context(), "hi"); err != nil {
		t.Fatal(err)
	}
	for range conv.ReceiveSteps(t.Context()) {
	}
	total := conv.TotalUsage()
	if deref(total.PromptTokenCount) != 15 || deref(total.TotalTokenCount) != 23 {
		t.Errorf("TotalUsage prompt=%d total=%d, want 15/23", deref(total.PromptTokenCount), deref(total.TotalTokenCount))
	}
	turn, ok := conv.LastTurnUsage()
	if !ok {
		t.Fatal("LastTurnUsage not present")
	}
	if deref(turn.PromptTokenCount) != 15 {
		t.Errorf("LastTurnUsage prompt = %d, want 15", deref(turn.PromptTokenCount))
	}
}

func TestConversationSendRecordsTurnAndPrompt(t *testing.T) {
	fc := &connection.FakeConnection{}
	conv := NewConversation(fc, 0)
	if err := conv.Send(t.Context(), "hello"); err != nil {
		t.Fatal(err)
	}
	if conv.TurnCount() != 1 {
		t.Errorf("TurnCount = %d, want 1", conv.TurnCount())
	}
	if p := fc.Prompts(); len(p) != 1 || p[0] != "hello" {
		t.Errorf("connection prompts = %v, want [hello]", p)
	}
}

func TestConversationChatReturnsImmediately(t *testing.T) {
	steps := []agtypes.Step{
		{StepIndex: 0, Source: agtypes.StepSourceModel, Target: agtypes.StepTargetUser, ContentDelta: "Hi"},
		{StepIndex: 0, Source: agtypes.StepSourceModel, Target: agtypes.StepTargetUser, ContentDelta: " there"},
	}
	conv := NewConversation(&connection.FakeConnection{Steps: steps}, 0)
	resp, err := conv.Chat(t.Context(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	// The stream has not been consumed yet; Text drives it.
	text, err := resp.Text()
	if err != nil {
		t.Fatal(err)
	}
	if text != "Hi there" {
		t.Errorf("Chat Text = %q, want %q", text, "Hi there")
	}
}

func TestConversationReceiveChunksDedupsToolCalls(t *testing.T) {
	steps := []agtypes.Step{
		{StepIndex: 0, ToolCalls: []agtypes.ToolCall{{Name: "t", ID: "a"}}},
		{StepIndex: 1, ToolCalls: []agtypes.ToolCall{{Name: "t", ID: "a"}}},              // duplicate id
		{StepIndex: 2, ToolCalls: []agtypes.ToolCall{{Name: "t", ID: "b"}, {Name: "t"}}}, // new + id-less
	}
	conv := NewConversation(&connection.FakeConnection{Steps: steps}, 0)
	var calls int
	for chunk, err := range conv.ReceiveChunks(t.Context()) {
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := chunk.(agtypes.ToolCall); ok {
			calls++
		}
	}
	// a (once), b (once), and the id-less call = 3.
	if calls != 3 {
		t.Errorf("yielded %d tool calls, want 3 (id 'a' deduped)", calls)
	}
}

func TestConversationSingleActiveIterator(t *testing.T) {
	// A blocking connection whose ReceiveSteps does not end lets us hold the
	// iterator open while starting a second iteration. The release channel is
	// closed at the end so the blocked seq goroutine returns (iter.Pull2's stop
	// alone cannot unblock a goroutine parked in a select).
	bc := &blockingConn{release: make(chan struct{})}
	conv := NewConversation(bc, 0)
	first := conv.ReceiveSteps(t.Context())
	next, stop := pull2Steps(first)
	defer func() {
		close(bc.release) // unblock the parked seq goroutine
		stop()            // then let iter.Pull2 observe the return
	}()
	next() // begins iteration, acquires the guard

	// A second iteration must immediately yield ErrIterating.
	var gotErr error
	for _, err := range conv.ReceiveSteps(t.Context()) {
		gotErr = err
		break
	}
	if gotErr != ErrIterating {
		t.Errorf("second iteration error = %v, want ErrIterating", gotErr)
	}
}
