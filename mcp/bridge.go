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

// Package mcp bridges Model Context Protocol servers to the SDK's tool runner.
//
// A Bridge connects to one or more MCP servers (over stdio, SSE, or streamable
// HTTP), discovers their tools, and exposes each as a tool.ToolWithSchema whose
// invocation forwards to the originating server. The Agent registers these
// tools alongside its host-side tools.
//
// It is built on the official MCP Go SDK
// (github.com/modelcontextprotocol/go-sdk/mcp).
package mcp

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/go-json-experiment/json"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/tool"
)

// clientName and clientVersion identify this client to MCP servers.
const (
	clientName    = "antigravity-sdk-go"
	clientVersion = "v0.0.0"
)

// Bridge manages the lifecycle of MCP client sessions and the tools discovered
// from them. The zero value is not ready; use NewBridge.
//
// A Bridge is not safe for concurrent Connect calls; connect all servers from a
// single goroutine (the Agent does this during startup), then read Tools.
type Bridge struct {
	client   *mcpsdk.Client
	sessions []*mcpsdk.ClientSession
	tools    []tool.ToolWithSchema
}

// NewBridge returns an empty Bridge ready to Connect to MCP servers.
func NewBridge() *Bridge {
	return &Bridge{
		client: mcpsdk.NewClient(&mcpsdk.Implementation{Name: clientName, Version: clientVersion}, nil),
	}
}

// Tools returns a copy of the tools discovered from all connected servers.
func (b *Bridge) Tools() []tool.ToolWithSchema {
	return append([]tool.ToolWithSchema(nil), b.tools...)
}

// Connect connects to the MCP server described by cfg, dispatching on its
// transport type, then refreshes the discovered tool set. It returns an error
// for an unrecognized configuration type or any connection/discovery failure.
func (b *Bridge) Connect(ctx context.Context, cfg agtypes.McpServerConfig) error {
	var transport mcpsdk.Transport
	switch c := cfg.(type) {
	case agtypes.McpStdioServer:
		cmd := exec.CommandContext(ctx, c.Command, c.Args...)
		transport = &mcpsdk.CommandTransport{Command: cmd}
	case agtypes.McpSseServer:
		transport = &mcpsdk.SSEClientTransport{Endpoint: c.URL}
	case agtypes.McpStreamableHTTPServer:
		transport = &mcpsdk.StreamableClientTransport{Endpoint: c.URL}
	default:
		return fmt.Errorf("mcp: unsupported MCP server config type %T", cfg)
	}
	return b.connect(ctx, transport)
}

// connect establishes a session over transport, appends it to the bridge, and
// re-discovers tools across all sessions.
func (b *Bridge) connect(ctx context.Context, transport mcpsdk.Transport) error {
	session, err := b.client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("mcp: connect: %w", err)
	}
	b.sessions = append(b.sessions, session)
	tools, err := toolsFromSession(ctx, session)
	if err != nil {
		return err
	}
	b.tools = append(b.tools, tools...)
	return nil
}

// Stop closes every active session and clears the bridge's state. It returns
// the first close error encountered, after attempting to close all sessions.
func (b *Bridge) Stop() error {
	var firstErr error
	for _, s := range b.sessions {
		if err := s.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	b.sessions = nil
	b.tools = nil
	return firstErr
}

// toolsFromSession lists the tools advertised by session and adapts each into a
// tool.ToolWithSchema whose Fn forwards the call to the session.
func toolsFromSession(ctx context.Context, session *mcpsdk.ClientSession) ([]tool.ToolWithSchema, error) {
	res, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: list tools: %w", err)
	}
	out := make([]tool.ToolWithSchema, 0, len(res.Tools))
	for _, t := range res.Tools {
		schema, err := schemaToMap(t.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("mcp: tool %q input schema: %w", t.Name, err)
		}
		out = append(out, tool.ToolWithSchema{
			Fn:          callToolFunc(session, t.Name),
			InputSchema: schema,
		})
	}
	return out, nil
}

// callToolFunc returns a tool.Tool that invokes the named tool on session and
// adapts the MCP result into a plain Go value.
func callToolFunc(session *mcpsdk.ClientSession, name string) tool.Tool {
	return func(ctx context.Context, args map[string]any) (any, error) {
		res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			return nil, fmt.Errorf("mcp: call tool %q: %w", name, err)
		}
		if res.IsError {
			return nil, fmt.Errorf("mcp: tool %q reported an error: %s", name, contentText(res.Content))
		}
		if res.StructuredContent != nil {
			return res.StructuredContent, nil
		}
		return contentText(res.Content), nil
	}
}

// contentText concatenates the text of any TextContent items in a tool result,
// the common case for MCP tool output.
func contentText(content []mcpsdk.Content) string {
	var b strings.Builder
	for _, c := range content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// schemaToMap normalizes an MCP tool's InputSchema (typed as any: a value that
// marshals to a JSON schema) into the map[string]any shape tool.ToolWithSchema
// expects. A nil schema yields a nil map.
func schemaToMap(schema any) (map[string]any, error) {
	if schema == nil {
		return nil, nil
	}
	if m, ok := schema.(map[string]any); ok {
		return m, nil
	}
	b, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}
