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

package mcp

import (
	"context"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// echoIn is the typed input for the test server's echo tool.
type echoIn struct {
	Message string `json:"message" jsonschema:"the message to echo"`
}

// echoOut is the typed output for the echo tool.
type echoOut struct {
	Echo string `json:"echo"`
}

// startEchoServer runs an in-memory MCP server exposing a single "echo" tool
// and returns the client-side transport connected to it. The server runs until
// the test context is cancelled.
func startEchoServer(t *testing.T) mcpsdk.Transport {
	t.Helper()
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "echo-server", Version: "v1.0.0"}, nil)
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "echo", Description: "Echoes its input"},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, in echoIn) (*mcpsdk.CallToolResult, echoOut, error) {
			return nil, echoOut{Echo: in.Message}, nil
		})

	clientT, serverT := mcpsdk.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = server.Run(ctx, serverT) }()
	return clientT
}

func TestBridgeDiscoversAndCallsTool(t *testing.T) {
	clientT := startEchoServer(t)

	b := NewBridge()
	t.Cleanup(func() { _ = b.Stop() })

	if err := b.connect(t.Context(), clientT); err != nil {
		t.Fatalf("connect: %v", err)
	}

	tools := b.Tools()
	if len(tools) != 1 {
		t.Fatalf("discovered %d tools, want 1", len(tools))
	}
	if tools[0].InputSchema == nil {
		t.Error("discovered tool has no input schema")
	}

	// Invoke the discovered tool through the adapted tool.Tool function.
	result, err := tools[0].Fn(t.Context(), map[string]any{"message": "hi"})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	// The echo tool returns structured output {"echo":"hi"}.
	got, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", result)
	}
	if got["echo"] != "hi" {
		t.Errorf("echo result = %v, want hi", got["echo"])
	}
}

func TestBridgeStopClearsState(t *testing.T) {
	clientT := startEchoServer(t)
	b := NewBridge()
	if err := b.connect(t.Context(), clientT); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if len(b.Tools()) == 0 {
		t.Fatal("expected tools before Stop")
	}
	if err := b.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
	if len(b.Tools()) != 0 {
		t.Error("Tools not cleared after Stop")
	}
}
