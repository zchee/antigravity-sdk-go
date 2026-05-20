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

package connection

import (
	"fmt"
	"slices"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/hook"
	"github.com/zchee/antigravity-sdk-go/hook/policy"
	"github.com/zchee/antigravity-sdk-go/tool"
	"github.com/zchee/antigravity-sdk-go/trigger"
)

// AgentConfig is the configuration for an agent. Each backend defines a
// concrete config that embeds BaseAgentConfig (for the shared fields) and
// implements CreateStrategy to produce its ConnectionStrategy.
//
// The Agent reads the shared configuration through the getter methods and
// rewrites policies and capabilities through the setters during startup, so it
// programs against this interface without type-asserting to a concrete config.
type AgentConfig interface {
	// CreateStrategy builds the ConnectionStrategy for this config, wiring in the
	// fully-prepared tool and hook runners. It is called by the Agent after
	// runners and policies are set up.
	CreateStrategy(toolRunner *tool.Runner, hookRunner *hook.Runner) (ConnectionStrategy, error)

	// Shared configuration accessors.

	SystemInstructions() agtypes.SystemInstructions
	Capabilities() agtypes.CapabilitiesConfig
	Tools() []tool.ToolWithSchema
	Policies() []policy.Policy
	Hooks() []hook.Hook
	Triggers() []trigger.Trigger
	MCPServers() []agtypes.McpServerConfig
	Workspaces() []string
	ConversationID() string
	SaveDir() string
	AppDataDir() string
	ResponseSchema() string
	SkillsPaths() []string

	// Setters used by the Agent to apply derived configuration on its private
	// copy of the config (see Clone).

	SetCapabilities(agtypes.CapabilitiesConfig)
	SetPolicies([]policy.Policy)

	// Clone returns a deep copy suitable for the Agent to own and mutate. Value
	// fields are deep-copied; the hooks, triggers, and tools slices are copied
	// shallowly so the identity of user-provided callbacks is preserved (mirrors
	// the upstream model_copy(deep=True) plus original-list snapshot).
	Clone() AgentConfig

	// Validate applies the config's defaults and validation rules in place,
	// idempotently. The Agent calls it during construction so backends that
	// derive state from user fields — for example LocalAgentConfig, which
	// prepends workspace-scoping policies — are fully configured even when the
	// caller passes the config directly. It returns a *ValidationError on
	// invalid input. BaseAgentConfig provides a no-op default.
	Validate() error
}

// Validate is the no-op default; backends that need defaults or validation
// override it. It lets a plain BaseAgentConfig satisfy AgentConfig.
func (c *BaseAgentConfig) Validate() error { return nil }

// BaseAgentConfig holds the configuration fields shared by every backend's
// AgentConfig. Embed it in a concrete config; it implements every AgentConfig
// method except CreateStrategy and Clone, which the concrete config must
// provide (Clone because only the concrete type can copy its own extra fields).
type BaseAgentConfig struct {
	// SystemInstructionsValue overrides or augments the default system prompt.
	SystemInstructionsValue agtypes.SystemInstructions
	// CapabilitiesValue configures agent capabilities. The zero value enables the
	// read-only tool set, matching the upstream default.
	CapabilitiesValue agtypes.CapabilitiesConfig
	// ToolsValue are the host-side custom tools, with their schemas.
	ToolsValue []tool.ToolWithSchema
	// PoliciesValue are the tool-call policies to enforce.
	PoliciesValue []policy.Policy
	// HooksValue are the lifecycle hooks to register.
	HooksValue []hook.Hook
	// TriggersValue are the event triggers to run alongside the session.
	TriggersValue []trigger.Trigger
	// MCPServersValue configure MCP servers whose tools are bridged in.
	MCPServersValue []agtypes.McpServerConfig
	// WorkspacesValue are the workspace directories.
	WorkspacesValue []string
	// ConversationIDValue resumes an existing conversation when set.
	ConversationIDValue string
	// SaveDirValue is where conversation state is persisted.
	SaveDirValue string
	// AppDataDirValue is the application data directory.
	AppDataDirValue string
	// ResponseSchemaValue is a JSON-schema string for structured output. Build it
	// from a value with MarshalResponseSchema.
	ResponseSchemaValue string
	// SkillsPathsValue are paths to skill definitions.
	SkillsPathsValue []string
}

// DefaultCapabilities returns the upstream default capabilities for an
// AgentConfig: only the read-only builtin tools enabled.
func DefaultCapabilities() agtypes.CapabilitiesConfig {
	c := agtypes.DefaultCapabilitiesConfig()
	c.EnabledTools = agtypes.ReadOnlyTools()
	return c
}

func (c *BaseAgentConfig) SystemInstructions() agtypes.SystemInstructions {
	return c.SystemInstructionsValue
}
func (c *BaseAgentConfig) Capabilities() agtypes.CapabilitiesConfig { return c.CapabilitiesValue }
func (c *BaseAgentConfig) Tools() []tool.ToolWithSchema             { return c.ToolsValue }
func (c *BaseAgentConfig) Policies() []policy.Policy                { return c.PoliciesValue }
func (c *BaseAgentConfig) Hooks() []hook.Hook                       { return c.HooksValue }
func (c *BaseAgentConfig) Triggers() []trigger.Trigger              { return c.TriggersValue }
func (c *BaseAgentConfig) MCPServers() []agtypes.McpServerConfig    { return c.MCPServersValue }
func (c *BaseAgentConfig) Workspaces() []string                     { return c.WorkspacesValue }
func (c *BaseAgentConfig) ConversationID() string                   { return c.ConversationIDValue }
func (c *BaseAgentConfig) SaveDir() string                          { return c.SaveDirValue }
func (c *BaseAgentConfig) AppDataDir() string                       { return c.AppDataDirValue }
func (c *BaseAgentConfig) ResponseSchema() string                   { return c.ResponseSchemaValue }
func (c *BaseAgentConfig) SkillsPaths() []string                    { return c.SkillsPathsValue }

// SetCapabilities replaces the capabilities configuration.
func (c *BaseAgentConfig) SetCapabilities(v agtypes.CapabilitiesConfig) { c.CapabilitiesValue = v }

// SetPolicies replaces the tool-call policies.
func (c *BaseAgentConfig) SetPolicies(v []policy.Policy) { c.PoliciesValue = v }

// CloneInto returns a deep copy of the shared fields. A concrete config's Clone
// uses it to populate the embedded BaseAgentConfig of the copy.
//
// Value-typed fields (capabilities, MCP server configs, workspaces, skills
// paths, policies) are deep-copied; the hooks, triggers, and tools slices are
// copied shallowly to preserve the identity of user-provided callbacks, which
// the Agent relies on when snapshotting from the original config.
func (c *BaseAgentConfig) CloneInto() BaseAgentConfig {
	return BaseAgentConfig{
		SystemInstructionsValue: c.SystemInstructionsValue,
		CapabilitiesValue:       cloneCapabilities(c.CapabilitiesValue),
		ToolsValue:              slices.Clone(c.ToolsValue),
		PoliciesValue:           slices.Clone(c.PoliciesValue),
		HooksValue:              slices.Clone(c.HooksValue),
		TriggersValue:           slices.Clone(c.TriggersValue),
		MCPServersValue:         slices.Clone(c.MCPServersValue),
		WorkspacesValue:         slices.Clone(c.WorkspacesValue),
		ConversationIDValue:     c.ConversationIDValue,
		SaveDirValue:            c.SaveDirValue,
		AppDataDirValue:         c.AppDataDirValue,
		ResponseSchemaValue:     c.ResponseSchemaValue,
		SkillsPathsValue:        slices.Clone(c.SkillsPathsValue),
	}
}

// cloneCapabilities deep-copies the slice and pointer fields of a
// CapabilitiesConfig so the copy can be mutated independently.
func cloneCapabilities(c agtypes.CapabilitiesConfig) agtypes.CapabilitiesConfig {
	c.EnabledTools = slices.Clone(c.EnabledTools)
	c.DisabledTools = slices.Clone(c.DisabledTools)
	if c.CompactionThreshold != nil {
		v := *c.CompactionThreshold
		c.CompactionThreshold = &v
	}
	return c
}

// MarshalResponseSchema normalizes a structured-output schema into the JSON
// string form stored on a config. It accepts a JSON string (validated as JSON
// and returned as-is) or any value that marshals to a JSON object.
//
// It is the Go replacement for the upstream response_schema validator, minus
// the pydantic.BaseModel-subclass branch, which has no Go analog.
func MarshalResponseSchema(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	if s, ok := v.(string); ok {
		if !jsontext.Value(s).IsValid() {
			return "", fmt.Errorf("connection: response_schema string is not valid JSON")
		}
		return s, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("connection: marshal response_schema: %w", err)
	}
	return string(b), nil
}
