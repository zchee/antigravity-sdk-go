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

package connection

import (
	"context"
	"iter"
	"sync"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/hook"
	"github.com/zchee/antigravity-sdk-go/tool"
)

// FakeConnection is an in-memory Connection for tests and examples. It records
// what it is asked to send and replays a fixed script of steps from
// ReceiveSteps. It embeds BaseConnection for the lifecycle no-ops and overrides
// the three required methods, which also validates that BaseConnection covers
// exactly the optional surface.
//
// FakeConnection is safe for concurrent use.
type FakeConnection struct {
	BaseConnection

	// Steps is the script ReceiveSteps replays, in order.
	Steps []agtypes.Step
	// ConvID is returned by ConversationID.
	ConvID string

	mu          sync.Mutex
	prompts     []agtypes.Content
	toolResults [][]agtypes.ToolResult
	triggerMsgs []string
}

// ConversationID returns the configured conversation id.
func (f *FakeConnection) ConversationID() string { return f.ConvID }

// Send records the prompt.
func (f *FakeConnection) Send(_ context.Context, prompt agtypes.Content) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts = append(f.prompts, prompt)
	return nil
}

// ReceiveSteps replays the configured Steps, honoring context cancellation.
func (f *FakeConnection) ReceiveSteps(ctx context.Context) iter.Seq2[agtypes.Step, error] {
	return func(yield func(agtypes.Step, error) bool) {
		for _, s := range f.Steps {
			if err := ctx.Err(); err != nil {
				yield(agtypes.Step{}, err)
				return
			}
			if !yield(s, nil) {
				return
			}
		}
	}
}

// SendToolResults records the results.
func (f *FakeConnection) SendToolResults(_ context.Context, results []agtypes.ToolResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.toolResults = append(f.toolResults, results)
	return nil
}

// SendTriggerNotification records the message.
func (f *FakeConnection) SendTriggerNotification(_ context.Context, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.triggerMsgs = append(f.triggerMsgs, content)
	return nil
}

// Prompts returns the prompts passed to Send, in order.
func (f *FakeConnection) Prompts() []agtypes.Content {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]agtypes.Content(nil), f.prompts...)
}

// TriggerMessages returns the messages passed to SendTriggerNotification.
func (f *FakeConnection) TriggerMessages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.triggerMsgs...)
}

// ToolResults returns the batches passed to SendToolResults.
func (f *FakeConnection) ToolResults() [][]agtypes.ToolResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]agtypes.ToolResult(nil), f.toolResults...)
}

// FakeStrategy is a ConnectionStrategy that hands out a fixed FakeConnection.
// It records the runners passed to Connect-time wiring and tracks lifecycle
// calls for tests.
type FakeStrategy struct {
	// Conn is returned by Connect after Start. If nil, Start creates an empty
	// FakeConnection.
	Conn *FakeConnection

	// ToolRunner and HookRunner capture the runners CreateStrategy received, so
	// a test holding the shared strategy can assert the Agent wired them — even
	// though the Agent operates on a clone of the config.
	ToolRunner *tool.Runner
	HookRunner *hook.Runner

	Started bool
	Closed  bool
}

// Start marks the strategy started, creating an empty connection if none was
// supplied.
func (s *FakeStrategy) Start(context.Context) error {
	if s.Conn == nil {
		s.Conn = &FakeConnection{}
	}
	s.Started = true
	return nil
}

// Connect returns the fake connection, or an error if Start has not run.
func (s *FakeStrategy) Connect() (Connection, error) {
	if !s.Started {
		return nil, ErrNotStarted
	}
	return s.Conn, nil
}

// Close marks the strategy closed.
func (s *FakeStrategy) Close(context.Context) error {
	s.Closed = true
	return nil
}

// FakeConfig is an AgentConfig backed by a FakeStrategy, for testing the Agent
// and Conversation layers without a real backend.
type FakeConfig struct {
	BaseAgentConfig

	// Strategy is returned by CreateStrategy. If nil, a fresh FakeStrategy is
	// created.
	Strategy *FakeStrategy

	// LastToolRunner and LastHookRunner capture the runners passed to
	// CreateStrategy, so tests can assert the Agent wired them.
	LastToolRunner *tool.Runner
	LastHookRunner *hook.Runner
}

// CreateStrategy returns the fake strategy, recording the runners.
func (c *FakeConfig) CreateStrategy(tr *tool.Runner, hr *hook.Runner) (ConnectionStrategy, error) {
	c.LastToolRunner = tr
	c.LastHookRunner = hr
	if c.Strategy == nil {
		c.Strategy = &FakeStrategy{}
	}
	c.Strategy.ToolRunner = tr
	c.Strategy.HookRunner = hr
	return c.Strategy, nil
}

// Clone returns a deep copy of the config, preserving the strategy reference.
func (c *FakeConfig) Clone() AgentConfig {
	return &FakeConfig{
		BaseAgentConfig: c.CloneInto(),
		Strategy:        c.Strategy,
	}
}

// Compile-time assertions that the fakes satisfy the package interfaces.
var (
	_ Connection         = (*FakeConnection)(nil)
	_ ConnectionStrategy = (*FakeStrategy)(nil)
	_ AgentConfig        = (*FakeConfig)(nil)
)

// toolConn and triggerNotifier mirror the unexported consumer interfaces in the
// tool and trigger packages (tool.conn, trigger.notifier). Asserting that
// Connection satisfies them here guarantees a *Connection can be passed where
// those packages expect their narrow interface — without exporting them or
// creating an import cycle (tool and trigger must not import connection).
//
// Keep these in sync with tool.conn / trigger.notifier; the assertions below
// fail to compile if Connection drifts from what those packages require.
type (
	toolConn interface {
		ConversationID() string
		IsIdle() bool
		SendTriggerNotification(ctx context.Context, message string) error
	}
	triggerNotifier interface {
		SendTriggerNotification(ctx context.Context, message string) error
	}
)

var (
	_ toolConn        = Connection(nil)
	_ triggerNotifier = Connection(nil)
)
