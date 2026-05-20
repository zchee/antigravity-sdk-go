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

package mcp_test

import (
	"strings"
	"testing"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/mcp"
)

// Note: the bridge's unsupported-config branch in Connect is unreachable from
// outside agtypes because McpServerConfig is a sealed interface — only the
// three known config types can be constructed. The branch remains as a guard
// for future config additions. We instead exercise a real dispatch path below.

func TestConnectStdioBadCommand(t *testing.T) {
	// A stdio config whose command does not exist should fail to connect rather
	// than panic; this exercises the stdio dispatch + error wrapping path.
	b := mcp.NewBridge()
	t.Cleanup(func() { _ = b.Stop() })
	cfg := agtypes.McpStdioServer{Command: "definitely-not-a-real-binary-xyz", Args: nil}
	err := b.Connect(t.Context(), cfg)
	if err == nil {
		t.Fatal("Connect with a nonexistent command = nil error, want failure")
	}
	if !strings.Contains(err.Error(), "mcp:") {
		t.Errorf("error %q not wrapped with package prefix", err)
	}
}
