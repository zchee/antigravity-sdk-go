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

// Package connection defines the transport-agnostic interfaces through which
// the SDK talks to an agent backend.
//
// A Connection is the SDK's public handle on a live agent session, regardless
// of where the agent runs; the Layer 2 APIs (Conversation, Agent) depend only
// on this interface, never on transport details. A ConnectionStrategy knows how
// to start a backend, hand out a Connection, and tear the backend down. An
// AgentConfig carries the user's configuration and produces the
// ConnectionStrategy for its backend.
package connection

import (
	"context"
	"errors"
	"iter"
	"time"

	"github.com/zchee/antigravity-sdk-go/agtypes"
)

// ErrNotStarted reports that a ConnectionStrategy's Connect was called before
// Start established the backend.
var ErrNotStarted = errors.New("connection: strategy not started")

// Connection is a live session with an agent backend — the common contract that
// every transport implements. Layer 2 APIs depend only on this interface.
//
// Embed BaseConnection to inherit no-op defaults for the lifecycle methods and
// implement only Send, ReceiveSteps, and SendTriggerNotification, which have no
// meaningful default.
type Connection interface {
	// IsIdle reports whether the connection is idle and ready for input.
	IsIdle() bool
	// ConversationID returns the conversation identifier, or "" if unset.
	ConversationID() string

	// Send sends a prompt to the agent. prompt is agent input (a string, an
	// agtypes.Media value, or a slice of those); see agtypes.Content.
	Send(ctx context.Context, prompt agtypes.Content) error

	// ReceiveSteps returns an iterator over steps as the agent produces them.
	// Iteration ends when the turn completes or ctx is cancelled; a non-nil
	// error for an element terminates iteration.
	ReceiveSteps(ctx context.Context) iter.Seq2[agtypes.Step, error]

	// Disconnect closes the session and releases its resources.
	Disconnect(ctx context.Context) error
	// Cancel cancels the turn in progress.
	Cancel(ctx context.Context) error
	// Delete deletes this connection and all associated backend state.
	Delete(ctx context.Context) error

	// SignalIdle signals that the connection is idle and ready for input.
	SignalIdle(ctx context.Context) error
	// WaitForIdle blocks until the connection becomes idle or ctx is cancelled.
	WaitForIdle(ctx context.Context) error
	// WaitForWakeup blocks until the connection wakes up or timeout elapses. It
	// returns true if the connection woke up, false on timeout.
	WaitForWakeup(ctx context.Context, timeout time.Duration) (bool, error)

	// SendToolResults sends tool execution results back to the agent.
	SendToolResults(ctx context.Context, results []agtypes.ToolResult) error
	// SendTriggerNotification sends a trigger message to the agent.
	SendTriggerNotification(ctx context.Context, content string) error
}

// ConnectionStrategy establishes a Connection to a specific backend and tears
// it down. Each backend type (local, cloud, …) provides its own
// implementation handling process management, transport, and auth.
//
// The lifecycle is Start → Connect (one or more times) → Close, mirroring the
// upstream async context manager (__aenter__ / connect / __aexit__).
type ConnectionStrategy interface {
	// Start brings up the backend and prepares it for connections. It must be
	// called before Connect.
	Start(ctx context.Context) error
	// Connect returns an established Connection. It returns an error if Start has
	// not run or the backend is not ready.
	Connect() (Connection, error)
	// Close tears down the backend and releases all resources. It is safe to
	// call after a failed Start.
	Close(ctx context.Context) error
}

// BaseConnection provides no-op implementations of the optional Connection
// lifecycle methods (everything except Send, ReceiveSteps, and
// SendTriggerNotification). Embed it in a concrete connection and override the
// methods the backend actually supports.
//
// It deliberately does not implement Send, ReceiveSteps, or
// SendTriggerNotification: those have no sensible default, so an embedding type
// must supply them (and the compiler enforces this when the type is used as a
// Connection).
type BaseConnection struct{}

// IsIdle reports the connection idle by default.
func (BaseConnection) IsIdle() bool { return true }

// ConversationID returns "" by default.
func (BaseConnection) ConversationID() string { return "" }

// Disconnect is a no-op by default.
func (BaseConnection) Disconnect(context.Context) error { return nil }

// Cancel is a no-op by default.
func (BaseConnection) Cancel(context.Context) error { return nil }

// Delete is a no-op by default.
func (BaseConnection) Delete(context.Context) error { return nil }

// SignalIdle is a no-op by default.
func (BaseConnection) SignalIdle(context.Context) error { return nil }

// WaitForIdle returns immediately by default.
func (BaseConnection) WaitForIdle(context.Context) error { return nil }

// WaitForWakeup reports a timeout (false) immediately by default.
func (BaseConnection) WaitForWakeup(context.Context, time.Duration) (bool, error) {
	return false, nil
}

// SendToolResults is a no-op by default.
func (BaseConnection) SendToolResults(context.Context, []agtypes.ToolResult) error { return nil }
