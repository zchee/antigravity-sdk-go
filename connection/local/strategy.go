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
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"time"

	"github.com/coder/websocket"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/connection"
	"github.com/zchee/antigravity-sdk-go/hook"
	pb "github.com/zchee/antigravity-sdk-go/internal/localharnesspb"
	"github.com/zchee/antigravity-sdk-go/tool"
)

// StrategyConfig carries the fully-prepared configuration the Strategy needs to
// bring up a LocalConnection. It is built by LocalAgentConfig.CreateStrategy.
type StrategyConfig struct {
	ToolRunner         *tool.Runner
	HookRunner         *hook.Runner
	GeminiConfig       agtypes.GeminiConfig
	SystemInstructions agtypes.SystemInstructions
	Capabilities       agtypes.CapabilitiesConfig
	ConversationID     string
	SaveDir            string
	Workspaces         []string
	AppDataDir         string
	SkillsPaths        []string
}

// Strategy establishes a LocalConnection: it resolves and spawns the
// localharness binary, performs the stdin/stdout handshake to learn the
// websocket port, connects, and sends the initialize event. It implements
// connection.ConnectionStrategy.
type Strategy struct {
	cfg        StrategyConfig
	binaryPath string
	conn       *LocalConnection
}

// NewStrategy returns a Strategy for the given configuration. Workspaces are
// normalized to clean paths up front (mirroring the upstream constructor).
func NewStrategy(cfg StrategyConfig) *Strategy {
	cfg.Workspaces = slices.Clone(cfg.Workspaces)
	for i, ws := range cfg.Workspaces {
		cfg.Workspaces[i] = normalizeWirePath(ws)
	}
	return &Strategy{cfg: cfg}
}

// Compile-time check that Strategy satisfies the ConnectionStrategy interface.
var _ connection.ConnectionStrategy = (*Strategy)(nil)

// wsConnectRetries and wsConnectBaseDelay control the websocket dial backoff,
// matching the upstream 5-attempt exponential schedule.
const (
	wsConnectRetries   = 5
	wsConnectBaseDelay = 100 * time.Millisecond
)

// Start spawns the harness, performs the handshake, connects the websocket, and
// initializes the conversation. A Gemini API key is required (from GeminiConfig
// or $GEMINI_API_KEY); without one the harness silently returns empty
// responses, so this fails fast.
func (s *Strategy) Start(ctx context.Context) error {
	apiKey := effectiveAPIKey(s.cfg.GeminiConfig)
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		return &agtypes.ValidationError{Message: "a Gemini API key is required: set GeminiConfig.APIKey or $GEMINI_API_KEY"}
	}

	binaryPath, err := resolveBinaryPath()
	if err != nil {
		return err
	}
	s.binaryPath = binaryPath

	harnessConfig, err := s.buildHarnessConfig()
	if err != nil {
		return err
	}

	cmd := exec.Command(binaryPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("local: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("local: stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("local: stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("local: start harness: %w", err)
	}
	stderr := newStderrBuffer(stderrPipe, 100)

	// Handshake: write InputConfig, read OutputConfig (port + api key), framed
	// little-endian.
	inputConfig := pb.InputConfig_builder{StorageDirectory: protoString(s.cfg.SaveDir)}.Build()
	if err := writeFrame(stdin, inputConfig); err != nil {
		return s.handshakeFail(cmd, stderr, err)
	}
	var outputConfig pb.OutputConfig
	if err := readFrame(stdout, &outputConfig); err != nil {
		return s.handshakeFail(cmd, stderr, err)
	}

	wsURL := fmt.Sprintf("ws://localhost:%d/", outputConfig.GetPort())
	ws, err := dialWithRetry(ctx, wsURL, outputConfig.GetApiKey())
	if err != nil {
		return s.handshakeFail(cmd, stderr, err)
	}

	initEvent := pb.InitializeConversationEvent_builder{Config: harnessConfig}.Build()
	c := newLocalConnection(ws, cmd, stdin, s.cfg.ToolRunner, s.cfg.HookRunner, stderr)
	if err := c.writeEvent(ctx, initEvent); err != nil {
		_ = c.Disconnect(ctx)
		return fmt.Errorf("local: initialize conversation: %w", err)
	}
	s.conn = c

	if s.cfg.HookRunner != nil {
		if err := s.cfg.HookRunner.DispatchSessionStart(ctx); err != nil {
			_ = c.Disconnect(ctx)
			return err
		}
	}
	return nil
}

// handshakeFail kills the harness and wraps err with its stderr tail.
func (s *Strategy) handshakeFail(cmd *exec.Cmd, stderr *stderrBuffer, err error) error {
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	tail := stderr.tail()
	if tail == "" {
		tail = "(no stderr output)"
	}
	return fmt.Errorf("local: harness handshake failed: %w\nharness stderr:\n%s", err, tail)
}

// dialWithRetry dials the harness websocket with exponential backoff, since the
// harness may need a moment to start listening after emitting OutputConfig.
func dialWithRetry(ctx context.Context, url, apiKey string) (*websocket.Conn, error) {
	header := http.Header{}
	header.Set("x-goog-api-key", apiKey)
	opts := &websocket.DialOptions{HTTPHeader: header}
	var lastErr error
	for attempt := range wsConnectRetries {
		ws, _, err := websocket.Dial(ctx, url, opts)
		if err == nil {
			return ws, nil
		}
		lastErr = err
		if attempt < wsConnectRetries-1 {
			delay := wsConnectBaseDelay * time.Duration(1<<attempt)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return nil, fmt.Errorf("local: dial websocket %s: %w", url, lastErr)
}

// Connect returns the established connection, or an error if Start has not run.
func (s *Strategy) Connect() (connection.Connection, error) {
	if s.conn == nil {
		return nil, connection.ErrNotStarted
	}
	return s.conn, nil
}

// Close tears down the connection if one was established.
func (s *Strategy) Close(ctx context.Context) error {
	if s.conn == nil {
		return nil
	}
	return s.conn.Disconnect(ctx)
}

// effectiveAPIKey returns the per-default-model key if set, else the shared key.
func effectiveAPIKey(g agtypes.GeminiConfig) string {
	if g.Models.Default.APIKey != "" {
		return g.Models.Default.APIKey
	}
	return g.APIKey
}

// buildHarnessConfig translates the SDK config into a HarnessConfig proto.
func (s *Strategy) buildHarnessConfig() (*pb.HarnessConfig, error) {
	toolProtos, err := s.toolProtos()
	if err != nil {
		return nil, err
	}

	b := pb.HarnessConfig_builder{
		CascadeId:            protoString(s.cfg.ConversationID),
		Tools:                toolProtos,
		Workspaces:           s.workspaceProtos(),
		SkillsPaths:          slices.Clone(s.cfg.SkillsPaths),
		HarnessSideTools:     s.harnessSideTools(),
		FinishToolSchemaJson: protoString(s.cfg.Capabilities.FinishToolSchemaJSON),
		AppDataDir:           protoString(s.cfg.AppDataDir),
		GeminiConfig:         s.geminiConfigProto(),
	}
	if si := s.systemInstructionsProto(); si != nil {
		b.SystemInstructions = si
	}
	// 0 tells the harness to use its default compaction threshold.
	if t := s.cfg.Capabilities.CompactionThreshold; t != nil && *t > 0 {
		b.CompactionThreshold = protoUint32(uint32(*t))
	} else {
		b.CompactionThreshold = protoUint32(0)
	}
	return b.Build(), nil
}

// toolProtos converts the registered host tools into Tool protos.
func (s *Strategy) toolProtos() ([]*pb.Tool, error) {
	if s.cfg.ToolRunner == nil {
		return nil, nil
	}
	names := s.cfg.ToolRunner.Names()
	out := make([]*pb.Tool, 0, len(names))
	for _, name := range names {
		schema, _ := s.cfg.ToolRunner.Schema(name)
		t, err := toolProto(name, "", schema)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// workspaceProtos builds filesystem-workspace protos from the configured paths.
func (s *Strategy) workspaceProtos() []*pb.Workspace {
	out := make([]*pb.Workspace, 0, len(s.cfg.Workspaces))
	for _, p := range s.cfg.Workspaces {
		fw := pb.FilesystemWorkspace_builder{Directory: protoString(p)}.Build()
		out = append(out, pb.Workspace_builder{FilesystemWorkspace: fw}.Build())
	}
	return out
}

// geminiConfigProto builds the GeminiConfig proto from the default model entry.
func (s *Strategy) geminiConfigProto() *pb.GeminiConfig {
	g := s.cfg.GeminiConfig
	b := pb.GeminiConfig_builder{ModelName: protoString(g.Models.Default.Name)}
	if key := effectiveAPIKey(g); key != "" {
		b.ApiKey = protoString(key)
	}
	if tl := g.Models.Default.Generation.ThinkingLevel; tl != "" {
		b.ThinkingLevel = protoString(string(tl))
	}
	return b.Build()
}

// systemInstructionsProto builds the SystemInstructions proto, or nil if none.
func (s *Strategy) systemInstructionsProto() *pb.SystemInstructions {
	switch si := s.cfg.SystemInstructions.(type) {
	case agtypes.CustomSystemInstructions:
		part := pb.CustomSystemInstructions_Part_builder{Text: protoString(si.Text)}.Build()
		custom := pb.CustomSystemInstructions_builder{Part: []*pb.CustomSystemInstructions_Part{part}}.Build()
		return pb.SystemInstructions_builder{Custom: custom}.Build()
	case agtypes.TemplatedSystemInstructions:
		ab := pb.AppendedSystemInstructions_builder{}
		if si.Identity != "" {
			ab.CustomIdentity = protoString(si.Identity)
		}
		sections := make([]*pb.AppendedSystemInstructions_Section, 0, len(si.Sections))
		for _, sec := range si.Sections {
			sections = append(sections, pb.AppendedSystemInstructions_Section_builder{
				Title:   protoString(sec.Title),
				Content: protoString(sec.Content),
			}.Build())
		}
		ab.AppendedSections = sections
		return pb.SystemInstructions_builder{Appended: ab.Build()}.Build()
	default:
		return nil
	}
}

// harnessSideTools builds the HarnessSideTools proto reflecting which builtin
// tools are active per the capabilities config.
func (s *Strategy) harnessSideTools() *pb.HarnessSideTools {
	active := activeBuiltinTools(s.cfg.Capabilities)
	has := func(t agtypes.BuiltinTools) bool { _, ok := active[t]; return ok }

	subagentEnabled := s.cfg.Capabilities.EnableSubagents && has(agtypes.ToolStartSubagent)
	return pb.HarnessSideTools_builder{
		Subagents:     pb.SubagentsConfig_builder{Enabled: protoBool(subagentEnabled)}.Build(),
		Find:          pb.FindToolConfig_builder{Enabled: protoBool(has(agtypes.ToolFindFile))}.Build(),
		UserQuestions: pb.UserQuestionsConfig_builder{Enabled: protoBool(has(agtypes.ToolAskQuestion))}.Build(),
		RunCommand:    pb.RunCommandToolConfig_builder{Enabled: protoBool(has(agtypes.ToolRunCommand))}.Build(),
		FileEdit:      pb.FileEditToolConfig_builder{Enabled: protoBool(has(agtypes.ToolEditFile))}.Build(),
		ViewFile:      pb.ViewFileToolConfig_builder{Enabled: protoBool(has(agtypes.ToolViewFile))}.Build(),
		WriteToFile:   pb.WriteToFileToolConfig_builder{Enabled: protoBool(has(agtypes.ToolCreateFile))}.Build(),
		GrepSearch:    pb.GrepSearchToolConfig_builder{Enabled: protoBool(has(agtypes.ToolSearchDir))}.Build(),
		ListDir:       pb.ListDirToolConfig_builder{Enabled: protoBool(has(agtypes.ToolListDir))}.Build(),
		GenerateImage: pb.GenerateImageToolConfig_builder{
			Enabled:   protoBool(has(agtypes.ToolGenerateImage)),
			ModelName: protoString(s.cfg.Capabilities.ImageModel),
		}.Build(),
	}.Build()
}

// activeBuiltinTools resolves the active builtin tool set from enabled/disabled
// lists: enabled is an allowlist; disabled is a denylist subtracted from all;
// neither means all tools.
func activeBuiltinTools(cfg agtypes.CapabilitiesConfig) map[agtypes.BuiltinTools]struct{} {
	active := make(map[agtypes.BuiltinTools]struct{})
	switch {
	case cfg.EnabledTools != nil:
		for _, t := range cfg.EnabledTools {
			active[t] = struct{}{}
		}
	case cfg.DisabledTools != nil:
		for _, t := range agtypes.AllTools() {
			active[t] = struct{}{}
		}
		for _, t := range cfg.DisabledTools {
			delete(active, t)
		}
	default:
		for _, t := range agtypes.AllTools() {
			active[t] = struct{}{}
		}
	}
	return active
}
