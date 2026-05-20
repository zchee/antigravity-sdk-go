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

package trigger

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

// entry is a registered trigger paired with its logging name.
type entry struct {
	name string
	fn   Trigger
}

// Runner manages the lifecycle of registered triggers. It starts each trigger
// as its own goroutine at Start and cancels them all at Stop. An unhandled
// error from a trigger is logged but does not crash the session or restart the
// trigger.
//
// A Runner is reusable: after Stop it can be started again.
type Runner struct {
	conn notifier

	mu      sync.Mutex
	entries []entry
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
}

// NewRunner returns a Runner that delivers trigger messages through n. Register
// triggers before calling Start.
func NewRunner(n notifier) *Runner {
	return &Runner{conn: n}
}

// Register adds a trigger with a logging name. Triggers must be registered
// before Start; registering while running returns ErrRunning.
func (r *Runner) Register(name string, fn Trigger) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return ErrRunning
	}
	if name == "" {
		name = "unknown"
	}
	r.entries = append(r.entries, entry{name: name, fn: fn})
	return nil
}

// ErrRunning reports an operation that is invalid while the Runner is running.
var ErrRunning = errors.New("trigger: runner is already started")

// Start launches every registered trigger as a goroutine, each with its own
// Context. It returns ErrRunning if already started. Triggers run with no
// ordering guarantees.
func (r *Runner) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return ErrRunning
	}
	runCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.running = true
	for _, e := range r.entries {
		r.wg.Go(func() {
			runTrigger(runCtx, e, NewContext(r.conn))
		})
	}
	return nil
}

// Stop cancels all trigger goroutines and waits for them to finish. It is safe
// to call multiple times; after Stop the Runner may be started again.
func (r *Runner) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	cancel := r.cancel
	r.mu.Unlock()

	cancel()
	r.wg.Wait()

	r.mu.Lock()
	r.running = false
	r.cancel = nil
	r.mu.Unlock()
}

// IsRunning reports whether the Runner has been started and not yet stopped.
func (r *Runner) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// runTrigger runs a single trigger with error handling: a context-cancellation
// error is expected on shutdown and logged at debug level; any other error is
// logged and swallowed so it cannot affect sibling triggers or the session.
func runTrigger(ctx context.Context, e entry, tc *Context) {
	err := e.fn(ctx, tc)
	switch {
	case err == nil:
		return
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		slog.Debug("trigger cancelled", "trigger", e.name)
	default:
		slog.Error("trigger failed with unhandled error", "trigger", e.name, "err", err)
	}
}
