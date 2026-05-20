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

package trigger_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/trigger"
)

// fakeConn records messages sent through it.
type fakeConn struct {
	mu   sync.Mutex
	sent []string
}

func (f *fakeConn) SendTriggerNotification(_ context.Context, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	return nil
}

func (f *fakeConn) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func TestContextSend(t *testing.T) {
	fc := &fakeConn{}
	c := trigger.NewContext(fc)
	if err := c.Send(t.Context(), "hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if fc.count() != 1 || fc.sent[0] != "hello" {
		t.Errorf("sent = %v, want [hello]", fc.sent)
	}
}

func TestEveryInvalidInterval(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Every(0) did not panic")
		}
	}()
	trigger.Every(0, func(context.Context, *trigger.Context) error { return nil })
}

func TestEveryFiresUntilCancel(t *testing.T) {
	var fires atomic.Int64
	tr := trigger.Every(2*time.Millisecond, func(context.Context, *trigger.Context) error {
		fires.Add(1)
		return nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- tr(ctx, trigger.NewContext(&fakeConn{})) }()

	// Allow several intervals to elapse, then cancel.
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("trigger returned %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("trigger did not stop after cancel")
	}
	if fires.Load() == 0 {
		t.Error("trigger never fired")
	}
}

func TestEveryPropagatesCallbackError(t *testing.T) {
	sentinel := errors.New("callback failed")
	tr := trigger.Every(time.Millisecond, func(context.Context, *trigger.Context) error {
		return sentinel
	})
	err := tr(t.Context(), trigger.NewContext(&fakeConn{}))
	if !errors.Is(err, sentinel) {
		t.Errorf("Every trigger returned %v, want %v", err, sentinel)
	}
}

func TestOnFileChange(t *testing.T) {
	dir := t.TempDir()
	changes := make(chan []agtypes.FileChange, 4)
	tr := trigger.OnFileChange(dir, func(_ context.Context, _ *trigger.Context, c []agtypes.FileChange) error {
		changes <- c
		return nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = tr(ctx, trigger.NewContext(&fakeConn{})) }()

	// Give the watcher a moment to register before mutating the directory.
	time.Sleep(50 * time.Millisecond)
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case c := <-changes:
		if len(c) == 0 {
			t.Fatal("empty change batch")
		}
		if c[0].Kind != agtypes.FileChangeAdded {
			t.Errorf("kind = %v, want added", c[0].Kind)
		}
		if filepath.Base(c[0].Path) != "f.txt" {
			t.Errorf("path = %q, want .../f.txt", c[0].Path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no file change event received")
	}
}

func TestRunnerLifecycle(t *testing.T) {
	r := trigger.NewRunner(&fakeConn{})
	var started atomic.Int64
	block := func(ctx context.Context, _ *trigger.Context) error {
		started.Add(1)
		<-ctx.Done()
		return ctx.Err()
	}
	if err := r.Register("a", block); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("b", block); err != nil {
		t.Fatal(err)
	}
	if r.IsRunning() {
		t.Error("IsRunning before Start")
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !r.IsRunning() {
		t.Error("not running after Start")
	}
	// Registering while running must fail.
	if err := r.Register("c", block); !errors.Is(err, trigger.ErrRunning) {
		t.Errorf("Register while running = %v, want ErrRunning", err)
	}
	// Double Start must fail.
	if err := r.Start(t.Context()); !errors.Is(err, trigger.ErrRunning) {
		t.Errorf("double Start = %v, want ErrRunning", err)
	}
	r.Stop()
	if r.IsRunning() {
		t.Error("running after Stop")
	}
	if started.Load() != 2 {
		t.Errorf("started %d triggers, want 2", started.Load())
	}
	// Stop is idempotent and the runner is reusable.
	r.Stop()
	if err := r.Start(t.Context()); err != nil {
		t.Errorf("restart after Stop: %v", err)
	}
	r.Stop()
}

func TestRunnerSwallowsTriggerError(t *testing.T) {
	r := trigger.NewRunner(&fakeConn{})
	done := make(chan struct{})
	_ = r.Register("failer", func(context.Context, *trigger.Context) error {
		defer close(done)
		return errors.New("boom")
	})
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("trigger did not run")
	}
	// A failing trigger must not affect Stop or the runner state.
	r.Stop()
	if r.IsRunning() {
		t.Error("runner still running after a trigger errored")
	}
}
