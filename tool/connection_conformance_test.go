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

package tool_test

import (
	"testing"

	"github.com/zchee/antigravity-sdk-go/connection"
	"github.com/zchee/antigravity-sdk-go/tool"
)

// TestConnectionSatisfiesToolConn verifies that a connection.Connection can be
// used wherever the tool package expects its (unexported) narrow connection
// interface. Passing a real *connection.FakeConnection into NewToolContext
// exercises the actual tool.conn interface, so this fails to compile or run if
// Connection ever drifts from what ToolContext requires. The conformance check
// lives in an external test package to avoid an import cycle (tool must not
// import connection in non-test code).
func TestConnectionSatisfiesToolConn(t *testing.T) {
	conn := &connection.FakeConnection{ConvID: "conv-xyz", Steps: nil}
	tc := tool.NewToolContext(conn)
	if got := tc.ConversationID(); got != "conv-xyz" {
		t.Errorf("ToolContext.ConversationID() = %q, want conv-xyz", got)
	}
	if err := tc.Send(t.Context(), "ping"); err != nil {
		t.Fatalf("ToolContext.Send: %v", err)
	}
	if msgs := conn.TriggerMessages(); len(msgs) != 1 || msgs[0] != "ping" {
		t.Errorf("connection trigger messages = %v, want [ping]", msgs)
	}
}
