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
	"fmt"
	"time"

	"github.com/fswatcher/fswatcher"
	"github.com/zchee/antigravity-sdk-go/agtypes"
)

// Every returns a Trigger that invokes callback once per interval. The first
// invocation happens after the first interval elapses, not immediately,
// matching the upstream every() helper. The returned trigger runs until its
// context is cancelled.
//
// It panics if interval is not positive, mirroring the upstream ValueError.
func Every(interval time.Duration, callback func(ctx context.Context, tc *Context) error) Trigger {
	if interval <= 0 {
		panic(fmt.Sprintf("trigger: Every interval must be positive, got %v", interval))
	}
	return func(ctx context.Context, tc *Context) error {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				if err := callback(ctx, tc); err != nil {
					return err
				}
			}
		}
	}
}

// fswatcherToKind maps an fswatcher operation to a FileChangeKind. Create maps
// to added, Remove and Rename to deleted, and Write/Chmod (and anything else)
// to modified — matching the watchfiles-derived semantics upstream relies on.
func fswatcherToKind(op fswatcher.Op) agtypes.FileChangeKind {
	switch {
	case op.Has(fswatcher.Create):
		return agtypes.FileChangeAdded
	case op.Has(fswatcher.Remove), op.Has(fswatcher.Rename):
		return agtypes.FileChangeDeleted
	default:
		return agtypes.FileChangeModified
	}
}

// OnFileChange returns a Trigger that invokes callback whenever the file or
// directory at path changes. Raw filesystem events are converted to
// agtypes.FileChange values before being passed to the callback. The trigger
// runs until its context is cancelled.
//
// Unlike the upstream lazily-imported watchfiles dependency, file watching is
// provided by fswatcher and is always available. The watcher observes the given
// path's direct entries; pass a directory to observe its children, or a file to
// observe that file.
func OnFileChange(path string, callback func(ctx context.Context, tc *Context, changes []agtypes.FileChange) error) Trigger {
	return func(ctx context.Context, tc *Context) error {
		w, err := fswatcher.NewWatcher()
		if err != nil {
			return fmt.Errorf("trigger: create file watcher: %w", err)
		}
		defer w.Close()
		if err := w.Add(path, fswatcher.All); err != nil {
			return fmt.Errorf("trigger: watch %q: %w", path, err)
		}
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case ev, ok := <-w.Events:
				if !ok {
					return nil
				}
				changes := []agtypes.FileChange{{
					Kind: fswatcherToKind(ev.Op),
					Path: ev.Name,
				}}
				if err := callback(ctx, tc, changes); err != nil {
					return err
				}
			case err, ok := <-w.Errors:
				if !ok {
					return nil
				}
				return fmt.Errorf("trigger: watch %q: %w", path, err)
			}
		}
	}
}
