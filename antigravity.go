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

// Package antigravity is a Go SDK for building AI agents on Google Antigravity
// and Gemini. It is a port of the Python google-antigravity/antigravity-sdk.
//
// The high-level entry point is Agent: construct one with New(ctx, config) and
// drive it with Chat. The default backend is the local harness, configured via
// LocalAgentConfig. This package is a thin facade that re-exports the public
// SDK types from their implementing packages so callers can use them as
// antigravity.X; the Agent and Conversation types are defined here.
package antigravity

import (
	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/connection"
	"github.com/zchee/antigravity-sdk-go/connection/local"
	"github.com/zchee/antigravity-sdk-go/tool"
)

// Re-exported public types, mirroring the upstream package __all__ so callers
// reference them as antigravity.X. Agent, Conversation, and ChatResponse are
// defined in this package directly.
type (
	// AgentConfig is the configuration contract a backend implements.
	AgentConfig = connection.AgentConfig
	// LocalAgentConfig configures the default local-harness backend.
	LocalAgentConfig = local.AgentConfig
	// ToolContext is the conversation-aware handle injected into tools.
	ToolContext = tool.ToolContext

	// ThinkingLevel controls extended-thinking effort.
	ThinkingLevel = agtypes.ThinkingLevel
	// CapabilitiesConfig configures agent capabilities and tool exposure.
	CapabilitiesConfig = agtypes.CapabilitiesConfig
	// GeminiConfig configures the Gemini backend.
	GeminiConfig = agtypes.GeminiConfig
	// GenerationConfig holds per-model generation parameters.
	GenerationConfig = agtypes.GenerationConfig
	// ModelConfig selects a model per capability.
	ModelConfig = agtypes.ModelConfig
	// ModelEntry is a model with optional auth and generation overrides.
	ModelEntry = agtypes.ModelEntry
	// UsageMetadata reports token usage from the model API.
	UsageMetadata = agtypes.UsageMetadata
)
