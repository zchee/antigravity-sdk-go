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

package agtypes

import "errors"

// Default model names.
const (
	// DefaultModel is the default primary reasoning model.
	DefaultModel = "gemini-3.5-flash"
	// DefaultImageGenerationModel is the default model used for image generation.
	DefaultImageGenerationModel = "gemini-3.1-flash-image-preview"
)

// GenerationConfig holds generation parameters for a model.
type GenerationConfig struct {
	// ThinkingLevel is the thinking level for models that support extended
	// thinking. When empty, the model's default level is used.
	ThinkingLevel ThinkingLevel `json:"thinking_level,omitzero"`
}

// ModelEntry is a model with optional auth and generation overrides.
//
// Upstream accepts a bare model-name string that Pydantic coerces into a
// ModelEntry. The Go port does not perform that implicit coercion; construct
// entries explicitly with NewModelEntry or a struct literal. This is a
// deliberate parity gap favoring explicitness.
type ModelEntry struct {
	// Name is the model name (e.g. "gemini-3.1-pro-preview").
	Name string `json:"name"`
	// APIKey is a per-model API key override. Falls back to GeminiConfig.APIKey.
	APIKey string `json:"api_key,omitzero"`
	// Generation holds generation parameters for this model.
	Generation GenerationConfig `json:"generation,omitzero"`
}

// NewModelEntry returns a ModelEntry for the named model with default
// generation parameters. It mirrors the upstream string-to-ModelEntry coercion.
func NewModelEntry(name string) ModelEntry {
	return ModelEntry{Name: name}
}

// ModelConfig selects a model for each capability.
type ModelConfig struct {
	// Default is the primary reasoning model.
	Default ModelEntry `json:"default,omitzero"`
	// ImageGeneration is the model used for image generation.
	ImageGeneration ModelEntry `json:"image_generation,omitzero"`
}

// DefaultModelConfig returns a ModelConfig populated with the default models,
// matching the upstream default_factory behavior.
func DefaultModelConfig() ModelConfig {
	return ModelConfig{
		Default:         NewModelEntry(DefaultModel),
		ImageGeneration: NewModelEntry(DefaultImageGenerationModel),
	}
}

// GeminiConfig configures the Gemini model backend.
type GeminiConfig struct {
	// APIKey is the shared API key for all models. Falls back to $GEMINI_API_KEY
	// if not set. Individual ModelEntry values can override this.
	APIKey string `json:"api_key,omitzero"`
	// Models holds per-modality model selection and configuration.
	Models ModelConfig `json:"models,omitzero"`
}

// SystemInstructionSection is a named section to append to the system
// instructions.
type SystemInstructionSection struct {
	Content string `json:"content"`
	// Title defaults to "user_system_instructions" when constructed via
	// NewSystemInstructionSection.
	Title string `json:"title,omitzero"`
}

// DefaultSystemInstructionSectionTitle is the default section title.
const DefaultSystemInstructionSectionTitle = "user_system_instructions"

// NewSystemInstructionSection returns a section with the default title.
func NewSystemInstructionSection(content string) SystemInstructionSection {
	return SystemInstructionSection{
		Content: content,
		Title:   DefaultSystemInstructionSectionTitle,
	}
}

// SystemInstructions is the sum type representing the two ways to configure
// system instructions:
//
//   - CustomSystemInstructions: full replacement (advanced usage).
//   - TemplatedSystemInstructions: append to defaults (recommended).
//
// Implementations are closed to this package via the unexported marker method.
type SystemInstructions interface {
	isSystemInstructions()
}

// CustomSystemInstructions completely replaces the system instructions.
//
// For advanced usage only: this replaces ALL default instructions. The caller
// is then responsible for providing all necessary instructions (core mandates,
// engineering standards, operational guidelines). Most users should use
// TemplatedSystemInstructions instead.
type CustomSystemInstructions struct {
	Text string `json:"text"`
}

func (CustomSystemInstructions) isSystemInstructions() {}

// TemplatedSystemInstructions overrides the agent's identity and appends
// sections to the default system instructions.
type TemplatedSystemInstructions struct {
	// Identity overrides the agent's identity when non-empty.
	Identity string `json:"identity,omitzero"`
	// Sections are appended to the default system instructions.
	Sections []SystemInstructionSection `json:"sections,omitzero"`
}

func (TemplatedSystemInstructions) isSystemInstructions() {}

// CapabilitiesConfig holds general agent capability configuration.
//
// Disabling vs. denying tools: EnabledTools/DisabledTools control which tools
// the harness exposes to the model — a disabled tool is stripped from the
// model's context entirely. By contrast the policy system (see package
// hook/policy) leaves a tool visible but rejects the call at runtime. Prefer
// DisabledTools/EnabledTools for tools the agent should never use; use a deny
// policy for conditional or context-dependent restrictions.
type CapabilitiesConfig struct {
	// EnableSubagents reports whether the agent can spawn and delegate to
	// sub-agents.
	EnableSubagents bool `json:"enable_subagents,omitzero"`
	// EnabledTools is an explicit allowlist of builtin tools to enable. Mutually
	// exclusive with DisabledTools. When nil, the harness defaults are used (all
	// tools enabled).
	EnabledTools []BuiltinTools `json:"enabled_tools,omitzero"`
	// DisabledTools is an explicit denylist of builtin tools to disable. Mutually
	// exclusive with EnabledTools. When nil, the harness defaults are used.
	DisabledTools []BuiltinTools `json:"disabled_tools,omitzero"`
	// CompactionThreshold is the token count after which the context window may
	// be compacted. When nil, the backend's default is used.
	CompactionThreshold *int `json:"compaction_threshold,omitzero"`
	// ImageModel is the model to use for image generation.
	ImageModel string `json:"image_model,omitzero"`
	// FinishToolSchemaJSON is an optional JSON schema string for the finish tool.
	FinishToolSchemaJSON string `json:"finish_tool_schema_json,omitzero"`
}

// DefaultCapabilitiesConfig returns a CapabilitiesConfig matching the upstream
// field defaults (subagents enabled, default image model).
func DefaultCapabilitiesConfig() CapabilitiesConfig {
	return CapabilitiesConfig{
		EnableSubagents: true,
		ImageModel:      DefaultImageGenerationModel,
	}
}

// ErrToolsMutuallyExclusive reports that both EnabledTools and DisabledTools
// were set on a CapabilitiesConfig.
var ErrToolsMutuallyExclusive = errors.New("agtypes: enabled_tools and disabled_tools should be mutually exclusive")

// Validate reports whether the configuration is internally consistent. It
// mirrors the upstream Pydantic after-validator: EnabledTools and DisabledTools
// must not both be set.
func (c CapabilitiesConfig) Validate() error {
	if c.EnabledTools != nil && c.DisabledTools != nil {
		return ErrToolsMutuallyExclusive
	}
	return nil
}

// McpServerConfig is the sum type for MCP server configurations, discriminated
// by the Type field on each implementation. Implementations are closed to this
// package via the unexported marker method.
type McpServerConfig interface {
	isMcpServerConfig()
	// MCPType returns the connection type discriminator ("stdio", "sse", "http").
	MCPType() string
}

// McpStdioServer configures an MCP server connected via stdio.
type McpStdioServer struct {
	// Command is the command to run to start the server.
	Command string `json:"command"`
	// Type is the connection type, always "stdio".
	Type string `json:"type,omitzero"`
	// Args are the arguments to pass to the command.
	Args []string `json:"args,omitzero"`
}

func (McpStdioServer) isMcpServerConfig() {}

// MCPType returns "stdio".
func (McpStdioServer) MCPType() string { return "stdio" }

// McpSseServer configures an MCP server connected via Server-Sent Events.
type McpSseServer struct {
	// URL is the SSE endpoint.
	URL string `json:"url"`
	// Type is the connection type, always "sse".
	Type string `json:"type,omitzero"`
	// Headers are optional headers to send with the connection request.
	Headers map[string]string `json:"headers,omitzero"`
}

func (McpSseServer) isMcpServerConfig() {}

// MCPType returns "sse".
func (McpSseServer) MCPType() string { return "sse" }

// McpStreamableHTTPServer configures an MCP server connected via streamable
// HTTP.
type McpStreamableHTTPServer struct {
	// URL is the HTTP endpoint.
	URL string `json:"url"`
	// Type is the connection type, always "http".
	Type string `json:"type,omitzero"`
	// Headers are optional headers to send with the connection request.
	Headers map[string]string `json:"headers,omitzero"`
	// Timeout is the connection timeout in seconds.
	Timeout float64 `json:"timeout,omitzero"`
	// SSEReadTimeout is the SSE read timeout in seconds.
	SSEReadTimeout float64 `json:"sse_read_timeout,omitzero"`
	// TerminateOnClose reports whether to terminate the connection on close.
	TerminateOnClose bool `json:"terminate_on_close,omitzero"`
}

func (McpStreamableHTTPServer) isMcpServerConfig() {}

// MCPType returns "http".
func (McpStreamableHTTPServer) MCPType() string { return "http" }

// Upstream streamable-HTTP defaults.
const (
	defaultMcpHTTPTimeout        = 30.0
	defaultMcpHTTPSSEReadTimeout = 300.0
)

// NewMcpStreamableHTTPServer returns a streamable-HTTP server config with the
// upstream defaults applied (timeout 30s, SSE read timeout 300s, terminate on
// close).
func NewMcpStreamableHTTPServer(url string) McpStreamableHTTPServer {
	return McpStreamableHTTPServer{
		URL:              url,
		Type:             "http",
		Timeout:          defaultMcpHTTPTimeout,
		SSEReadTimeout:   defaultMcpHTTPSSEReadTimeout,
		TerminateOnClose: true,
	}
}
