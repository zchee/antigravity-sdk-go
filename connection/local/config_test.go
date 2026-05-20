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

package local_test

import (
	"errors"
	"testing"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/connection"
	"github.com/zchee/antigravity-sdk-go/connection/local"
	"github.com/zchee/antigravity-sdk-go/hook/policy"
)

func TestAgentConfigShorthands(t *testing.T) {
	cfg, err := (&local.AgentConfig{Model: "gemini-x", APIKey: "k"}).Build()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GeminiConfig.Models.Default.Name != "gemini-x" {
		t.Errorf("model shorthand not applied: %q", cfg.GeminiConfig.Models.Default.Name)
	}
	if cfg.GeminiConfig.APIKey != "k" {
		t.Errorf("api key shorthand not applied: %q", cfg.GeminiConfig.APIKey)
	}
}

func TestAgentConfigShorthandConflicts(t *testing.T) {
	tests := map[string]*local.AgentConfig{
		"model conflict": {
			Model:        "a",
			GeminiConfig: agtypes.GeminiConfig{Models: agtypes.ModelConfig{Default: agtypes.NewModelEntry("b")}},
		},
		"api key conflict": {
			APIKey:       "a",
			GeminiConfig: agtypes.GeminiConfig{APIKey: "b"},
		},
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := cfg.Build()
			var verr *agtypes.ValidationError
			if !errors.As(err, &verr) {
				t.Errorf("Build() error = %v, want *ValidationError", err)
			}
		})
	}
}

func TestAgentConfigAppDataDirMustBeAbsolute(t *testing.T) {
	cfg := &local.AgentConfig{}
	cfg.AppDataDirValue = "relative/path"
	if _, err := cfg.Build(); err == nil {
		t.Error("Build() with relative AppDataDir = nil error, want validation error")
	}
}

// TestAgentConfigPrependsWorkspacePolicies is the security-relevant check: when
// workspaces are configured, workspace-scoping deny policies must be prepended
// ahead of the user's policies so file operations stay confined.
func TestAgentConfigPrependsWorkspacePolicies(t *testing.T) {
	cfg := &local.AgentConfig{}
	cfg.WorkspacesValue = []string{t.TempDir()}
	cfg.PoliciesValue = []policy.Policy{policy.AllowAll()}

	built, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}
	policies := built.Policies()
	if len(policies) <= 1 {
		t.Fatalf("expected workspace policies prepended, got %d policies", len(policies))
	}
	// The leading policies must be the workspace_only deny policies for file
	// tools, ahead of the user's AllowAll.
	if policies[0].Name != "workspace_only" {
		t.Errorf("first policy = %q, want workspace_only (prepended)", policies[0].Name)
	}
	if last := policies[len(policies)-1]; last.Decision != policy.Approve {
		t.Errorf("last policy decision = %v, want the user's AllowAll", last.Decision)
	}
}

func TestAgentConfigDefaultPolicies(t *testing.T) {
	// With no explicit policies, ConfirmRunCommand is the default (and workspace
	// policies prepend ahead of it since cwd is the default workspace).
	cfg := &local.AgentConfig{}
	cfg.WorkspacesValue = []string{} // disable workspace prepending to isolate the default
	built, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(built.Policies()) == 0 {
		t.Fatal("expected default ConfirmRunCommand policies")
	}
}

func TestAgentConfigCloneIsDeep(t *testing.T) {
	cfg := &local.AgentConfig{Model: "m"}
	cfg.WorkspacesValue = []string{"/ws"}
	clone := cfg.Clone()
	lc, ok := clone.(*local.AgentConfig)
	if !ok {
		t.Fatalf("Clone returned %T, want *local.AgentConfig", clone)
	}
	if lc.Model != "m" {
		t.Errorf("clone Model = %q, want m", lc.Model)
	}
	// Mutating the clone's workspaces must not affect the original.
	lc.WorkspacesValue[0] = "/mutated"
	if cfg.WorkspacesValue[0] != "/ws" {
		t.Error("Clone did not deep-copy Workspaces")
	}
}

// TestStrategyStartRequiresAPIKey confirms the fail-fast on a missing API key
// without needing the binary (the check runs before any spawn).
func TestStrategyStartRequiresAPIKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	s := local.NewStrategy(local.StrategyConfig{})
	err := s.Start(t.Context())
	var verr *agtypes.ValidationError
	if !errors.As(err, &verr) {
		t.Errorf("Start without API key = %v, want *ValidationError", err)
	}
}

func TestStrategyConnectBeforeStart(t *testing.T) {
	s := local.NewStrategy(local.StrategyConfig{})
	if _, err := s.Connect(); !errors.Is(err, connection.ErrNotStarted) {
		t.Errorf("Connect before Start = %v, want ErrNotStarted", err)
	}
}
