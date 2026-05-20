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

package agtypes_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"

	"github.com/go-json-experiment/json"

	"github.com/zchee/antigravity-sdk-go/agtypes"
)

func TestBuiltinToolGroupings(t *testing.T) {
	tests := map[string]struct {
		got  []agtypes.BuiltinTools
		want []agtypes.BuiltinTools
	}{
		"read_only": {
			got: agtypes.ReadOnlyTools(),
			want: []agtypes.BuiltinTools{
				agtypes.ToolListDir, agtypes.ToolSearchDir, agtypes.ToolFindFile,
				agtypes.ToolViewFile, agtypes.ToolFinish,
			},
		},
		"file_tools": {
			got:  agtypes.FileTools(),
			want: []agtypes.BuiltinTools{agtypes.ToolViewFile, agtypes.ToolCreateFile, agtypes.ToolEditFile},
		},
		"none": {
			got:  agtypes.NoTools(),
			want: []agtypes.BuiltinTools{},
		},
		"all_has_every_tool": {
			got: agtypes.AllTools(),
			want: []agtypes.BuiltinTools{
				agtypes.ToolListDir, agtypes.ToolSearchDir, agtypes.ToolFindFile,
				agtypes.ToolViewFile, agtypes.ToolCreateFile, agtypes.ToolEditFile,
				agtypes.ToolRunCommand, agtypes.ToolAskQuestion, agtypes.ToolStartSubagent,
				agtypes.ToolGenerateImage, agtypes.ToolFinish,
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if diff := gocmp.Diff(tc.want, tc.got); diff != "" {
				t.Errorf("tool grouping mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCapabilitiesConfigValidate(t *testing.T) {
	tools := []agtypes.BuiltinTools{agtypes.ToolViewFile}
	tests := map[string]struct {
		cfg     agtypes.CapabilitiesConfig
		wantErr error
	}{
		"valid: neither set":   {cfg: agtypes.CapabilitiesConfig{}, wantErr: nil},
		"valid: only enabled":  {cfg: agtypes.CapabilitiesConfig{EnabledTools: tools}, wantErr: nil},
		"valid: only disabled": {cfg: agtypes.CapabilitiesConfig{DisabledTools: tools}, wantErr: nil},
		"error: both set": {
			cfg:     agtypes.CapabilitiesConfig{EnabledTools: tools, DisabledTools: tools},
			wantErr: agtypes.ErrToolsMutuallyExclusive,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Validate() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestStepExtraRoundTrip confirms unknown JSON members round-trip through the
// inline Extra fallback (the upstream Pydantic extra="allow" behavior).
func TestStepExtraRoundTrip(t *testing.T) {
	raw := []byte(`{"id":"s1","step_index":3,"type":"TOOL_CALL","cascade_id":"c-9","custom_flag":true}`)
	var step agtypes.Step
	if err := json.Unmarshal(raw, &step); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if step.ID != "s1" || step.StepIndex != 3 || step.Type != agtypes.StepTypeToolCall {
		t.Errorf("named fields not decoded: %+v", step)
	}
	if got := step.Extra["cascade_id"]; got != "c-9" {
		t.Errorf("Extra[cascade_id] = %v, want c-9", got)
	}
	if got := step.Extra["custom_flag"]; got != true {
		t.Errorf("Extra[custom_flag] = %v, want true", got)
	}
	// Re-marshal must preserve the unknown members.
	out, err := json.Marshal(step)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var rt agtypes.Step
	if err := json.Unmarshal(out, &rt); err != nil {
		t.Fatalf("Unmarshal round-trip: %v", err)
	}
	if diff := gocmp.Diff(step, rt); diff != "" {
		t.Errorf("Step round-trip mismatch (-want +got):\n%s", diff)
	}
}

// TestOptionalOmitzero confirms Pydantic-style optional semantics: nil pointers
// are omitted, a pointer to the zero value is emitted.
func TestOptionalOmitzero(t *testing.T) {
	tests := map[string]struct {
		um   agtypes.UsageMetadata
		want string
	}{
		"all nil": {um: agtypes.UsageMetadata{}, want: `{}`},
		"explicit zero": {
			um:   agtypes.UsageMetadata{PromptTokenCount: new(0)},
			want: `{"prompt_token_count":0}`,
		},
		"set value": {
			um:   agtypes.UsageMetadata{TotalTokenCount: new(42)},
			want: `{"total_token_count":42}`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			out, err := json.Marshal(tc.um)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(out) != tc.want {
				t.Errorf("Marshal = %s, want %s", out, tc.want)
			}
		})
	}
}

func TestFromFile(t *testing.T) {
	dir := t.TempDir()
	tests := map[string]struct {
		filename string
		content  []byte
		wantMIME string
		wantErr  bool
	}{
		"png is image":      {filename: "a.png", content: []byte{0x89, 'P', 'N', 'G'}, wantMIME: "image/png"},
		"pdf is document":   {filename: "a.pdf", content: []byte("%PDF"), wantMIME: "application/pdf"},
		"unknown extension": {filename: "a.xyz", content: []byte("x"), wantErr: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(dir, tc.filename)
			if err := os.WriteFile(p, tc.content, 0o600); err != nil {
				t.Fatal(err)
			}
			m, err := agtypes.FromFile(p, "desc")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("FromFile(%q) = nil error, want error", tc.filename)
				}
				return
			}
			if err != nil {
				t.Fatalf("FromFile(%q): %v", tc.filename, err)
			}
			if m.MIME() != tc.wantMIME {
				t.Errorf("MIME = %q, want %q", m.MIME(), tc.wantMIME)
			}
			if m.Desc() != "desc" {
				t.Errorf("Desc = %q, want %q", m.Desc(), "desc")
			}
			if got := gocmp.Diff(string(tc.content), string(m.Bytes())); got != "" {
				t.Errorf("Bytes mismatch (-want +got):\n%s", got)
			}
		})
	}
}

func TestNewImageValidation(t *testing.T) {
	if _, err := agtypes.NewImage([]byte("x"), "image/png", ""); err != nil {
		t.Errorf("NewImage(png) unexpected error: %v", err)
	}
	if _, err := agtypes.NewImage([]byte("x"), "image/gif", ""); err == nil {
		t.Error("NewImage(gif) = nil error, want unsupported MIME error")
	}
}

// TestUpstreamNullEmptyParity confirms the JSON shapes match upstream Pydantic
// defaults: ToolCall.Args defaults to an empty object, and a nil
// ToolResult.Result / Step.StructuredOutput serializes as explicit null rather
// than being omitted.
func TestUpstreamNullEmptyParity(t *testing.T) {
	tests := map[string]struct {
		value any
		want  string
	}{
		"toolcall nil args -> empty object": {
			value: agtypes.ToolCall{Name: "t"},
			want:  `{"name":"t","args":{}}`,
		},
		"toolresult nil result -> null": {
			value: agtypes.ToolResult{Name: "t"},
			want:  `{"name":"t","result":null}`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			out, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(out) != tc.want {
				t.Errorf("Marshal = %s, want %s", out, tc.want)
			}
		})
	}
}

// TestStepStructuredOutputNull confirms Step.StructuredOutput serializes as
// explicit null when unset, matching upstream.
func TestStepStructuredOutputNull(t *testing.T) {
	out, err := json.Marshal(agtypes.Step{ID: "s"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// The named-but-not-omitted structured_output must appear as null.
	if !strings.Contains(string(out), `"structured_output":null`) {
		t.Errorf("Step JSON missing structured_output null: %s", out)
	}
}

func TestSystemInstructionsSumType(t *testing.T) {
	var si agtypes.SystemInstructions = agtypes.TemplatedSystemInstructions{Identity: "bot"}
	switch v := si.(type) {
	case agtypes.TemplatedSystemInstructions:
		if v.Identity != "bot" {
			t.Errorf("Identity = %q, want bot", v.Identity)
		}
	default:
		t.Errorf("unexpected type %T", v)
	}
}

func TestMcpServerConfigDiscriminator(t *testing.T) {
	tests := map[string]struct {
		cfg  agtypes.McpServerConfig
		want string
	}{
		"stdio": {cfg: agtypes.McpStdioServer{Command: "x"}, want: "stdio"},
		"sse":   {cfg: agtypes.McpSseServer{URL: "u"}, want: "sse"},
		"http":  {cfg: agtypes.NewMcpStreamableHTTPServer("u"), want: "http"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := tc.cfg.MCPType(); got != tc.want {
				t.Errorf("MCPType() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDefaults(t *testing.T) {
	mc := agtypes.DefaultModelConfig()
	if mc.Default.Name != agtypes.DefaultModel {
		t.Errorf("DefaultModelConfig().Default.Name = %q, want %q", mc.Default.Name, agtypes.DefaultModel)
	}
	if mc.ImageGeneration.Name != agtypes.DefaultImageGenerationModel {
		t.Errorf("DefaultModelConfig().ImageGeneration.Name = %q, want %q", mc.ImageGeneration.Name, agtypes.DefaultImageGenerationModel)
	}
	cap := agtypes.DefaultCapabilitiesConfig()
	if !cap.EnableSubagents {
		t.Error("DefaultCapabilitiesConfig().EnableSubagents = false, want true")
	}
	http := agtypes.NewMcpStreamableHTTPServer("u")
	if http.Timeout != 30.0 || http.SSEReadTimeout != 300.0 || !http.TerminateOnClose {
		t.Errorf("streamable HTTP defaults wrong: %+v", http)
	}
}
