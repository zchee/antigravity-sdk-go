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

package antigravity

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/connection"
	"github.com/zchee/antigravity-sdk-go/hook"
	"github.com/zchee/antigravity-sdk-go/hook/policy"
	"github.com/zchee/antigravity-sdk-go/mcp"
	"github.com/zchee/antigravity-sdk-go/tool"
	"github.com/zchee/antigravity-sdk-go/trigger"
)

// Agent is the Layer 1 SDK entry point. It owns a configured session: a
// connection, conversation, hook/tool/trigger runners, and an optional MCP
// bridge. Construct one with New, drive it with Chat (or the Conversation
// accessor), and release it with Close.
type Agent struct {
	config connection.AgentConfig

	started atomic.Bool

	mu              sync.Mutex
	pendingHooks    []hook.Hook
	pendingTriggers []trigger.Trigger

	hookRunner    *hook.Runner
	toolRunner    *tool.Runner
	triggerRunner *trigger.Runner
	mcpBridge     *mcp.Bridge
	strategy      connection.ConnectionStrategy
	conversation  *Conversation
}

// ErrNoPolicy reports that write tools or MCP servers are enabled without a
// safety policy or a custom pre-tool-call decide hook.
var ErrNoPolicy = errors.New(
	"antigravity: write tools or MCP servers are enabled without a safety policy; " +
		"add policy.AllowAll() to approve all tool calls, or policy.DenyAll() with " +
		"policy.Allow(\"tool_name\") to selectively allow tools",
)

// ErrAgentStarted reports an operation invalid after the agent has started.
var ErrAgentStarted = errors.New("antigravity: cannot register triggers after the agent has started")

// ErrAgentNotStarted reports use of a session accessor before New completed.
var ErrAgentNotStarted = errors.New("antigravity: agent session not started")

// NewAgent returns an unstarted Agent for config. The config is deep-copied
// (preserving user-callback identity per the AgentConfig.Clone contract), so
// later mutations of the caller's config do not affect the agent. Pending hooks
// and triggers are snapshotted from the clone. Call Start (or use New, which
// constructs and starts in one step).
func NewAgent(config connection.AgentConfig) *Agent {
	cfg := config.Clone()
	if rs := cfg.ResponseSchema(); rs != "" {
		caps := cfg.Capabilities()
		caps.FinishToolSchemaJSON = rs
		cfg.SetCapabilities(caps)
	}
	return &Agent{
		config:          cfg,
		pendingHooks:    cfg.Hooks(),
		pendingTriggers: cfg.Triggers(),
	}
}

// New constructs an Agent for config and starts its session, returning the
// started agent. The caller owns teardown: defer agent.Close(ctx).
func New(ctx context.Context, config connection.AgentConfig) (*Agent, error) {
	a := NewAgent(config)
	if err := a.Start(ctx); err != nil {
		return nil, err
	}
	return a, nil
}

// RegisterHook registers a hook. Before Start it is queued; after Start it is
// registered immediately with the running hook runner.
func (a *Agent) RegisterHook(h hook.Hook) error {
	if !a.started.Load() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.pendingHooks = append(a.pendingHooks, h)
		return nil
	}
	return a.hookRunner.Register(h)
}

// RegisterTrigger queues a trigger to run when the agent starts. It returns
// ErrAgentStarted if called after Start.
func (a *Agent) RegisterTrigger(t trigger.Trigger) error {
	if a.started.Load() {
		return ErrAgentStarted
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pendingTriggers = append(a.pendingTriggers, t)
	return nil
}

// Start brings up the session: it validates the config, builds the hook runner
// and registers pending hooks, enforces the safety-policy guard, connects MCP
// servers, builds the tool runner, creates the connection and conversation,
// starts triggers, and wires the ToolContext. On any failure it tears down what
// was started.
//
// Start is not retryable: after a failed Start, construct a new Agent rather
// than calling Start again (pending hooks/triggers are consumed on the first
// attempt).
func (a *Agent) Start(ctx context.Context) (err error) {
	slog.Info("starting agent session")

	// Apply the config's defaults and validation (idempotent) so a config passed
	// directly to New — without a prior Build — is fully configured, including
	// any backend-derived safety policies.
	if verr := a.config.Validate(); verr != nil {
		return verr
	}

	a.hookRunner = hook.NewRunner()

	a.mu.Lock()
	pendingHooks := a.pendingHooks
	a.pendingHooks = nil
	pendingTriggers := a.pendingTriggers
	a.mu.Unlock()

	for _, h := range pendingHooks {
		if rerr := a.hookRunner.Register(h); rerr != nil {
			return rerr
		}
	}

	if guardErr := a.applySafetyPolicy(); guardErr != nil {
		return guardErr
	}

	// On any failure past this point, tear down what we started.
	defer func() {
		if err != nil {
			_ = a.teardown(ctx)
		}
	}()

	allTools := a.config.Tools()
	if servers := a.config.MCPServers(); len(servers) > 0 {
		slog.Info("connecting to MCP servers", "count", len(servers))
		a.mcpBridge = mcp.NewBridge()
		for _, sc := range servers {
			if cerr := a.mcpBridge.Connect(ctx, sc); cerr != nil {
				return cerr
			}
		}
		allTools = append(allTools, a.mcpBridge.Tools()...)
	}

	a.toolRunner = tool.NewRunner()
	for _, t := range allTools {
		if rerr := a.toolRunner.AddTool(t); rerr != nil {
			return rerr
		}
	}

	a.strategy, err = a.config.CreateStrategy(a.toolRunner, a.hookRunner)
	if err != nil {
		return err
	}

	conv, err := CreateConversation(ctx, a.strategy)
	if err != nil {
		return err
	}
	a.conversation = conv

	if len(pendingTriggers) > 0 {
		slog.Info("starting triggers", "count", len(pendingTriggers))
		// conv.Connection() structurally satisfies the trigger runner's notifier
		// interface (SendTriggerNotification); see the conformance test in the
		// trigger package.
		a.triggerRunner = trigger.NewRunner(conv.Connection())
		for i, tr := range pendingTriggers {
			if rerr := a.triggerRunner.Register("trigger-"+strconv.Itoa(i), tr); rerr != nil {
				return rerr
			}
		}
		if serr := a.triggerRunner.Start(ctx); serr != nil {
			return serr
		}
	}

	// Wire the ToolContext so host tools can access conversation capabilities.
	a.toolRunner.SetContext(tool.NewToolContext(conv.Connection()))

	a.started.Store(true)
	return nil
}

// applySafetyPolicy registers the enforced policy hook and runs the guard that
// requires a safety policy (or a custom decide hook) when write tools or MCP
// servers are enabled.
func (a *Agent) applySafetyPolicy() error {
	policies := a.config.Policies()

	readOnly := make(map[agtypes.BuiltinTools]struct{})
	for _, t := range agtypes.ReadOnlyTools() {
		readOnly[t] = struct{}{}
	}
	hasWriteTools := false
	for _, t := range a.config.Capabilities().ActiveBuiltinTools() {
		if _, ro := readOnly[t]; !ro {
			hasWriteTools = true
			break
		}
	}
	hasMCP := len(a.config.MCPServers()) > 0

	if (hasWriteTools || hasMCP) && len(policies) == 0 && !a.hookRunner.HasPreToolCallDecide() {
		return ErrNoPolicy
	}

	if len(policies) > 0 {
		h, err := policy.Enforce(policies)
		if err != nil {
			return err
		}
		if rerr := a.hookRunner.Register(h); rerr != nil {
			return rerr
		}
	}
	return nil
}

// Close stops the agent session and releases its resources in reverse order of
// startup. It is safe to call on a partially-started agent. Errors from each
// teardown step are joined.
func (a *Agent) Close(ctx context.Context) error {
	return a.teardown(ctx)
}

// teardown stops triggers, disconnects the conversation (which dispatches the
// session-end hook and closes the connection), and stops the MCP bridge,
// joining any errors.
func (a *Agent) teardown(ctx context.Context) error {
	slog.Info("stopping agent session")
	var errs []error
	if a.triggerRunner != nil {
		a.triggerRunner.Stop()
		a.triggerRunner = nil
	}
	if a.conversation != nil {
		if err := a.conversation.Disconnect(ctx); err != nil {
			errs = append(errs, err)
		}
		a.conversation = nil
	}
	if a.mcpBridge != nil {
		if err := a.mcpBridge.Stop(); err != nil {
			errs = append(errs, err)
		}
		a.mcpBridge = nil
	}
	a.started.Store(false)
	return errors.Join(errs...)
}

// Chat sends a prompt and returns the streaming response. The agent must be
// started.
func (a *Agent) Chat(ctx context.Context, prompt agtypes.Content) (*ChatResponse, error) {
	if !a.started.Load() {
		return nil, ErrAgentNotStarted
	}
	return a.conversation.Chat(ctx, prompt)
}

// IsStarted reports whether the session has been started.
func (a *Agent) IsStarted() bool { return a.started.Load() }

// Conversation returns the active conversation for advanced introspection
// (history, usage, direct send/receive). It panics if the agent is not started;
// guard with IsStarted when in doubt.
func (a *Agent) Conversation() *Conversation {
	if !a.started.Load() {
		panic(ErrAgentNotStarted)
	}
	return a.conversation
}

// ConversationID returns the runtime-assigned conversation id, or "" before the
// session starts or before the runtime assigns one.
func (a *Agent) ConversationID() string {
	if !a.started.Load() || a.conversation == nil {
		return ""
	}
	return a.conversation.ConversationID()
}
