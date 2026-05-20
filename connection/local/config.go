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

package local

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/connection"
	"github.com/zchee/antigravity-sdk-go/hook"
	"github.com/zchee/antigravity-sdk-go/hook/policy"
	"github.com/zchee/antigravity-sdk-go/tool"
)

// AgentConfig configures the local-harness backend. It is the default Agent
// config and embeds connection.BaseAgentConfig for the shared fields.
//
// By default all tools are enabled but run_command is denied via
// policy.ConfirmRunCommand; pass Policies=[]policy.Policy{policy.AllowAll()}
// for autonomous shell access. When Workspaces is set, file tools are
// restricted to those directories via policy.WorkspaceOnly. Use Build to apply
// defaults and validators before constructing an Agent.
type AgentConfig struct {
	connection.BaseAgentConfig

	// GeminiConfig configures the Gemini backend.
	GeminiConfig agtypes.GeminiConfig

	// Model is a shorthand for GeminiConfig.Models.Default.Name; setting both is
	// an error.
	Model string
	// APIKey is a shorthand for GeminiConfig.APIKey; setting both is an error.
	APIKey string
}

// DefaultAppDataDir is the default application data directory, expanded from
// ~/.gemini/antigravity. It is added to the workspace allowlist so the harness
// can read and write its own state.
func DefaultAppDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".gemini/antigravity"
	}
	return filepath.Join(home, ".gemini", "antigravity")
}

// Build applies the local config defaults and validators, returning a config
// ready to drive an Agent. It mirrors the upstream Pydantic validators:
//
//   - applies the Model/APIKey shorthands to GeminiConfig (rejecting conflicts);
//   - defaults Workspaces to the current directory and Capabilities to the
//     upstream default;
//   - prepends workspace-scoping policies (always, so file ops stay confined)
//     and the confirm-run-command default policy when none were set;
//   - requires an absolute AppDataDir when one is given.
//
// Build returns a *connection.ValidationError on any conflict or invalid value.
func (c *AgentConfig) Build() (*AgentConfig, error) {
	out := *c
	out.BaseAgentConfig = c.BaseAgentConfig // shared base copied by value

	if err := out.applyShorthands(); err != nil {
		return nil, err
	}
	if err := out.applyDefaultsAndPolicies(); err != nil {
		return nil, err
	}
	return &out, nil
}

// applyShorthands folds Model/APIKey into GeminiConfig, rejecting conflicts.
func (c *AgentConfig) applyShorthands() error {
	if c.Model != "" {
		if c.GeminiConfig.Models.Default.Name != "" {
			return &agtypes.ValidationError{Message: "cannot set both Model shorthand and GeminiConfig.Models.Default.Name"}
		}
		c.GeminiConfig.Models.Default = agtypes.NewModelEntry(c.Model)
	}
	if c.APIKey != "" {
		if c.GeminiConfig.APIKey != "" {
			return &agtypes.ValidationError{Message: "cannot set both APIKey shorthand and GeminiConfig.APIKey"}
		}
		c.GeminiConfig.APIKey = c.APIKey
	}
	return nil
}

// applyDefaultsAndPolicies fills defaults and prepends workspace policies.
func (c *AgentConfig) applyDefaultsAndPolicies() error {
	if c.AppDataDirValue != "" && !filepath.IsAbs(c.AppDataDirValue) {
		return &agtypes.ValidationError{Message: fmt.Sprintf("AppDataDir must be an absolute path, got %q", c.AppDataDirValue)}
	}

	if c.GeminiConfig.Models.Default.Name == "" {
		c.GeminiConfig.Models.Default = agtypes.NewModelEntry(agtypes.DefaultModel)
	}
	if c.CapabilitiesValue.ImageModel == "" {
		c.CapabilitiesValue = agtypes.DefaultCapabilitiesConfig()
	}
	if c.WorkspacesValue == nil {
		if cwd, err := os.Getwd(); err == nil {
			c.WorkspacesValue = []string{cwd}
		}
	}
	if c.PoliciesValue == nil {
		c.PoliciesValue = policy.ConfirmRunCommand(nil)
	}

	// Always prepend workspace-scoping policies when workspaces are configured,
	// including the app data dir in the allowlist, so file ops stay confined.
	if len(c.WorkspacesValue) > 0 {
		appData := c.AppDataDirValue
		if appData == "" {
			appData = DefaultAppDataDir()
		}
		allowed := append(slices.Clone(c.WorkspacesValue), appData)
		c.PoliciesValue = append(policy.WorkspaceOnly(allowed), c.PoliciesValue...)
	}
	return nil
}

// CreateStrategy builds the local connection strategy, wiring in the prepared
// runners. It normalizes a string system-instruction to templated form and
// defaults SaveDir to a fresh temp directory, mirroring the upstream.
func (c *AgentConfig) CreateStrategy(toolRunner *tool.Runner, hookRunner *hook.Runner) (connection.ConnectionStrategy, error) {
	si := c.SystemInstructionsValue

	saveDir := c.SaveDirValue
	if saveDir == "" {
		dir, err := os.MkdirTemp("", "antigravity_")
		if err != nil {
			return nil, fmt.Errorf("local: create save dir: %w", err)
		}
		saveDir = dir
	}

	return NewStrategy(StrategyConfig{
		ToolRunner:         toolRunner,
		HookRunner:         hookRunner,
		GeminiConfig:       c.GeminiConfig,
		SystemInstructions: si,
		Capabilities:       c.CapabilitiesValue,
		ConversationID:     c.ConversationIDValue,
		SaveDir:            saveDir,
		Workspaces:         c.WorkspacesValue,
		AppDataDir:         c.AppDataDirValue,
		SkillsPaths:        c.SkillsPathsValue,
	}), nil
}

// Clone returns a deep copy suitable for the Agent to own and mutate, deep-
// copying the shared base (preserving hook/trigger identity) and the local
// fields.
func (c *AgentConfig) Clone() connection.AgentConfig {
	return &AgentConfig{
		BaseAgentConfig: c.CloneInto(),
		GeminiConfig:    c.GeminiConfig,
		Model:           c.Model,
		APIKey:          c.APIKey,
	}
}

// Compile-time check that *AgentConfig satisfies the AgentConfig interface.
var _ connection.AgentConfig = (*AgentConfig)(nil)
