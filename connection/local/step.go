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
	"net/url"
	"strconv"

	gojson "github.com/go-json-experiment/json"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	pb "github.com/zchee/antigravity-sdk-go/internal/localharnesspb"
)

// Extra-field keys used to carry LocalConnection-specific step data through
// agtypes.Step.Extra (which exists precisely for this; see the Step godoc).
const (
	ExtraCascadeID    = "cascade_id"
	ExtraTrajectoryID = "trajectory_id"
	ExtraWireTarget   = "wire_target"
	ExtraHTTPCode     = "http_code"
)

// pathArgKeys are the tool-argument fields that may carry a filesystem path the
// harness expresses in wire form. They are normalized to clean absolute paths
// and the result is recorded as the ToolCall's canonical path for policy
// evaluation. The set mirrors the upstream sanitizer.
var pathArgKeys = []string{"path", "file_path", "TargetFile", "directory_path"}

// normalizeWirePath translates a harness wire path into a clean filesystem
// path: a file:// URL becomes its (percent-decoded) path component; any other
// string is returned unchanged.
func normalizeWirePath(path string) string {
	u, err := url.Parse(path)
	if err == nil && u.Scheme == "file" {
		// url.Parse already percent-decodes u.Path.
		return u.Path
	}
	return path
}

// makeStepID builds a stable step identifier from a trajectory id and index,
// matching the upstream "traj:idx" (or bare index when the trajectory is empty)
// form.
func makeStepID(trajectoryID string, stepIndex uint32) string {
	if trajectoryID != "" {
		return trajectoryID + ":" + strconv.FormatUint(uint64(stepIndex), 10)
	}
	return strconv.FormatUint(uint64(stepIndex), 10)
}

// sourceMap and statusMap translate the proto enums to agtypes step enums.
var (
	sourceMap = map[pb.StepUpdate_Source]agtypes.StepSource{
		pb.StepUpdate_SOURCE_SYSTEM: agtypes.StepSourceSystem,
		pb.StepUpdate_SOURCE_USER:   agtypes.StepSourceUser,
		pb.StepUpdate_SOURCE_MODEL:  agtypes.StepSourceModel,
	}
	statusMap = map[pb.StepUpdate_State]agtypes.StepStatus{
		pb.StepUpdate_STATE_ACTIVE:           agtypes.StepStatusActive,
		pb.StepUpdate_STATE_DONE:             agtypes.StepStatusDone,
		pb.StepUpdate_STATE_WAITING_FOR_USER: agtypes.StepStatusWaitingForUser,
		pb.StepUpdate_STATE_ERROR:            agtypes.StepStatusError,
	}
)

// builtinToolProtoField pairs a builtin tool name with the StepUpdate accessor
// reporting whether that action is set, and the action message (for arg
// extraction). The order matters only for type determination; at most one
// action is set per StepUpdate.
type builtinAction struct {
	name string
	has  func(*pb.StepUpdate) bool
	msg  func(*pb.StepUpdate) proto.Message
}

var builtinActions = []builtinAction{
	{string(agtypes.ToolCreateFile), (*pb.StepUpdate).HasCreateFile, func(s *pb.StepUpdate) proto.Message { return s.GetCreateFile() }},
	{string(agtypes.ToolEditFile), (*pb.StepUpdate).HasEditFile, func(s *pb.StepUpdate) proto.Message { return s.GetEditFile() }},
	{string(agtypes.ToolFindFile), (*pb.StepUpdate).HasFindFile, func(s *pb.StepUpdate) proto.Message { return s.GetFindFile() }},
	{string(agtypes.ToolListDir), (*pb.StepUpdate).HasListDirectory, func(s *pb.StepUpdate) proto.Message { return s.GetListDirectory() }},
	{string(agtypes.ToolRunCommand), (*pb.StepUpdate).HasRunCommand, func(s *pb.StepUpdate) proto.Message { return s.GetRunCommand() }},
	{string(agtypes.ToolSearchDir), (*pb.StepUpdate).HasSearchDirectory, func(s *pb.StepUpdate) proto.Message { return s.GetSearchDirectory() }},
	{string(agtypes.ToolViewFile), (*pb.StepUpdate).HasViewFile, func(s *pb.StepUpdate) proto.Message { return s.GetViewFile() }},
	{string(agtypes.ToolStartSubagent), (*pb.StepUpdate).HasInvokeSubagent, func(s *pb.StepUpdate) proto.Message { return s.GetInvokeSubagent() }},
	{string(agtypes.ToolGenerateImage), (*pb.StepUpdate).HasGenerateImage, func(s *pb.StepUpdate) proto.Message { return s.GetGenerateImage() }},
	{string(agtypes.ToolFinish), (*pb.StepUpdate).HasFinish, func(s *pb.StepUpdate) proto.Message { return s.GetFinish() }},
}

// activeAction returns the active builtin action name and its message, or
// ("", nil) when the StepUpdate carries no builtin action.
func activeAction(s *pb.StepUpdate) (string, proto.Message) {
	for _, a := range builtinActions {
		if a.has(s) {
			return a.name, a.msg(s)
		}
	}
	return "", nil
}

// protoToMap converts a proto message to a map keyed by proto field name
// (preserving_proto_field_name semantics), via protojson.
func protoToMap(m proto.Message) (map[string]any, error) {
	b, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(m)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := gojson.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// stepFromUpdate converts a StepUpdate proto into an agtypes.Step, populating
// ToolCall.CanonicalPath for file tools (the field the workspace policy checks)
// and stashing the LocalConnection-specific fields in Step.Extra. usage is
// attached when non-nil.
func stepFromUpdate(s *pb.StepUpdate, usage *agtypes.UsageMetadata) (agtypes.Step, error) {
	trajID := s.GetTrajectoryId()
	stepIdx := int(s.GetStepIndex())
	id := makeStepID(trajID, s.GetStepIndex())

	var toolCalls []agtypes.ToolCall
	actionName, actionMsg := activeAction(s)
	if actionName != "" {
		args := map[string]any{}
		if actionMsg != nil && actionMsg.ProtoReflect().IsValid() {
			m, err := protoToMap(actionMsg)
			if err != nil {
				return agtypes.Step{}, fmt.Errorf("local: decode action %q args: %w", actionName, err)
			}
			args = m
		}
		canonical := normalizePathArgs(args)
		toolCalls = append(toolCalls, agtypes.ToolCall{
			Name:          actionName,
			Args:          args,
			ID:            id,
			CanonicalPath: canonical,
		})
	}

	stepType := stepTypeOf(s, actionName)
	source := sourceMap[s.GetSource()]
	if source == "" {
		source = agtypes.StepSourceUnknown
	}
	status := statusMap[s.GetState()]
	if status == "" {
		status = agtypes.StepStatusUnknown
	}
	wireTarget := s.GetTarget().String()

	isComplete := source == agtypes.StepSourceModel &&
		status == agtypes.StepStatusDone &&
		s.GetText() != "" &&
		wireTarget == "TARGET_USER"

	var structured any
	if stepType == agtypes.StepTypeFinish {
		if out := s.GetFinish().GetOutputString(); out != "" {
			// Best-effort parse; leave nil if not valid JSON (matches upstream).
			var v any
			if err := gojson.Unmarshal([]byte(out), &v); err == nil {
				structured = v
			}
		}
	}

	step := agtypes.Step{
		ID:               id,
		StepIndex:        stepIdx,
		Type:             stepType,
		Source:           source,
		Target:           targetOf(wireTarget),
		Status:           status,
		Content:          s.GetText(),
		ContentDelta:     s.GetTextDelta(),
		Thinking:         s.GetThinking(),
		ThinkingDelta:    s.GetThinkingDelta(),
		ToolCalls:        toolCalls,
		Error:            s.GetError().GetErrorMessage(),
		StructuredOutput: structured,
		UsageMetadata:    usage,
		Extra: map[string]any{
			ExtraCascadeID:    s.GetCascadeId(),
			ExtraTrajectoryID: trajID,
			ExtraWireTarget:   wireTarget,
			ExtraHTTPCode:     int(s.GetError().GetHttpCode()),
		},
	}
	if isComplete {
		t := true
		step.IsCompleteResponse = &t
	}
	return step, nil
}

// normalizePathArgs rewrites any known path argument in args to its clean form
// and returns the last such normalized path (the canonical path), or "" if none.
func normalizePathArgs(args map[string]any) string {
	var canonical string
	for _, key := range pathArgKeys {
		if v, ok := args[key].(string); ok {
			n := normalizeWirePath(v)
			args[key] = n
			canonical = n
		}
	}
	return canonical
}

// stepTypeOf determines the high-level step type, mirroring the upstream
// precedence: compaction, then finish, then any builtin tool action, then text.
func stepTypeOf(s *pb.StepUpdate, actionName string) agtypes.StepType {
	switch {
	case s.HasCompaction():
		return agtypes.StepTypeCompaction
	case s.HasFinish():
		return agtypes.StepTypeFinish
	case actionName != "":
		return agtypes.StepTypeToolCall
	case s.GetText() != "":
		return agtypes.StepTypeTextResponse
	default:
		return agtypes.StepTypeUnknown
	}
}

// targetOf maps the wire target string to the agtypes StepTarget.
func targetOf(wire string) agtypes.StepTarget {
	switch wire {
	case "TARGET_USER":
		return agtypes.StepTargetUser
	case "TARGET_ENVIRONMENT":
		return agtypes.StepTargetEnvironment
	case "TARGET_UNSPECIFIED":
		return agtypes.StepTargetUnspecified
	default:
		return agtypes.StepTargetUnknown
	}
}

// extractToolResult returns the structured result carried on a completed
// StepUpdate's action sub-message, or nil when no structured result applies.
// At most one action is set per StepUpdate.
func extractToolResult(s *pb.StepUpdate) ToolOutput {
	switch {
	case s.HasRunCommand():
		if out := s.GetRunCommand().GetCombinedOutput(); out != "" {
			return RunCommandResult{Output: out}
		}
	case s.HasListDirectory():
		results := s.GetListDirectory().GetResults()
		if len(results) > 0 {
			entries := make([]ListDirectoryEntry, 0, len(results))
			for _, r := range results {
				entries = append(entries, ListDirectoryEntry{
					Name:        r.GetName(),
					IsDirectory: r.GetIsDirectory(),
					FileSize:    int64(r.GetFileSize()),
				})
			}
			return ListDirectoryResult{Entries: entries}
		}
	case s.HasFindFile():
		if out := s.GetFindFile().GetOutput(); out != "" {
			return FindFileResult{Output: out}
		}
	case s.HasSearchDirectory():
		if n := s.GetSearchDirectory().GetNumResults(); n != 0 {
			return SearchDirectoryResult{NumResults: int(n)}
		}
	case s.HasEditFile():
		if len(s.GetEditFile().GetDiffBlock()) > 0 {
			return EditFileResult{Summary: s.GetText()}
		}
	case s.HasGenerateImage():
		if name := s.GetGenerateImage().GetImageName(); name != "" {
			return GenerateImageResult{ImageName: name}
		}
	}
	return nil
}

// parseUsageMetadata converts a proto UsageMetadata into agtypes.UsageMetadata,
// preserving the present/absent distinction (nil when a field is unset).
func parseUsageMetadata(u *pb.UsageMetadata) agtypes.UsageMetadata {
	var out agtypes.UsageMetadata
	if u.HasPromptTokenCount() {
		out.PromptTokenCount = ptrInt32(u.GetPromptTokenCount())
	}
	if u.HasCachedContentTokenCount() {
		out.CachedContentTokenCount = ptrInt32(u.GetCachedContentTokenCount())
	}
	if u.HasCandidatesTokenCount() {
		out.CandidatesTokenCount = ptrInt32(u.GetCandidatesTokenCount())
	}
	if u.HasThoughtsTokenCount() {
		out.ThoughtsTokenCount = ptrInt32(u.GetThoughtsTokenCount())
	}
	if u.HasTotalTokenCount() {
		out.TotalTokenCount = ptrInt32(u.GetTotalTokenCount())
	}
	return out
}

func ptrInt32(v int32) *int {
	i := int(v)
	return &i
}

// stepTracker tracks the wire state and handled requests for one trajectory
// step, preventing duplicate handling of confirmation/question requests that
// the harness rebroadcasts while waiting for the user.
//
// A stepTracker is NOT safe for concurrent use; the LocalConnection accesses
// every tracker only while holding its mutex.
type stepTracker struct {
	state           pb.StepUpdate_State
	handledRequests map[string]struct{}
}

func newStepTracker() *stepTracker {
	return &stepTracker{
		state:           pb.StepUpdate_STATE_UNSPECIFIED,
		handledRequests: make(map[string]struct{}),
	}
}

// updateState records a new state, clearing the handled-request set when
// leaving the waiting-for-user state.
func (t *stepTracker) updateState(s pb.StepUpdate_State) {
	if t.state == pb.StepUpdate_STATE_WAITING_FOR_USER && s != pb.StepUpdate_STATE_WAITING_FOR_USER {
		clear(t.handledRequests)
	}
	t.state = s
}

// markHandled records requestType as handled, returning true only the first
// time (so callers launch one handler per request despite rebroadcasts).
func (t *stepTracker) markHandled(requestType string) bool {
	if _, ok := t.handledRequests[requestType]; ok {
		return false
	}
	t.handledRequests[requestType] = struct{}{}
	return true
}
