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

// Package trigger provides long-lived event sources that run alongside an
// agent session and push messages back into the agent.
//
// A Trigger is a function that runs until its context is cancelled, reacting to
// external events (intervals, file changes, webhooks) and calling
// Context.Send to deliver messages to the agent. The Runner starts every
// trigger as its own goroutine at session start and cancels them at session
// end.
package trigger

import "context"

// notifier is the narrow slice of a connection that a trigger Context depends
// on. The full connection.Connection (defined in a later phase) structurally
// satisfies it. Declaring it locally keeps this package dependent only on the
// standard library (plus agtypes via the helpers) and follows the
// accept-interfaces idiom.
type notifier interface {
	// SendTriggerNotification injects a trigger-style notification into the
	// conversation.
	SendTriggerNotification(ctx context.Context, message string) error
}

// Context is the handle provided to every trigger. It exposes the capability to
// send messages to the agent. One Context is created per trigger.
type Context struct {
	conn notifier
}

// NewContext returns a trigger Context that delivers messages through n.
func NewContext(n notifier) *Context {
	return &Context{conn: n}
}

// Send delivers content to the agent as a trigger notification.
func (c *Context) Send(ctx context.Context, content string) error {
	return c.conn.SendTriggerNotification(ctx, content)
}

// Trigger is a long-lived function that runs alongside an agent session. It
// should run until ctx is cancelled, using tc.Send to push messages to the
// agent, and return ctx.Err() (or nil) when ctx is done.
//
// Upstream uses an async function plus a trigger() decorator that validates the
// signature and tags the function. In Go the function type is the contract, so
// no decorator is needed; pass a name to Runner.Register (or use the Every /
// OnFileChange helpers, which name themselves) to label a trigger for logging.
type Trigger func(ctx context.Context, tc *Context) error
