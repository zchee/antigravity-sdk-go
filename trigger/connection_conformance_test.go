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
	"testing"

	"github.com/zchee/antigravity-sdk-go/connection"
	"github.com/zchee/antigravity-sdk-go/trigger"
)

// TestConnectionSatisfiesNotifier verifies that a connection.Connection can be
// used wherever the trigger package expects its (unexported) notifier
// interface. Passing a real *connection.FakeConnection into NewContext
// exercises the actual trigger.notifier interface, catching any drift. The
// check lives in an external test package to avoid an import cycle (trigger
// must not import connection in non-test code).
func TestConnectionSatisfiesNotifier(t *testing.T) {
	conn := &connection.FakeConnection{}
	tc := trigger.NewContext(conn)
	if err := tc.Send(t.Context(), "hello"); err != nil {
		t.Fatalf("trigger Context.Send: %v", err)
	}
	if msgs := conn.TriggerMessages(); len(msgs) != 1 || msgs[0] != "hello" {
		t.Errorf("connection trigger messages = %v, want [hello]", msgs)
	}
}
