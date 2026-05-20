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

// The proto fixtures in this file are SYNTHETIC: they are constructed from the
// generated localharness proto builders, not captured from the real
// localharness binary. They verify structural correctness of the parsing logic
// (field mapping, path normalization, type determination), NOT ground-truth
// wire fidelity. When a binary is available, the gated integration test
// (connection_integration_test.go) exercises the real wire format.
package local

import (
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	pb "github.com/zchee/antigravity-sdk-go/internal/localharnesspb"
)

func TestNormalizeWirePath(t *testing.T) {
	tests := map[string]struct {
		in   string
		want string
	}{
		"file url":            {in: "file:///abs/path/f.txt", want: "/abs/path/f.txt"},
		"file url with space": {in: "file:///abs/my%20file.txt", want: "/abs/my file.txt"},
		"plain abs path":      {in: "/already/clean", want: "/already/clean"},
		"relative path":       {in: "rel/path", want: "rel/path"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := normalizeWirePath(tc.in); got != tc.want {
				t.Errorf("normalizeWirePath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMakeStepID(t *testing.T) {
	if got := makeStepID("traj", 5); got != "traj:5" {
		t.Errorf("makeStepID = %q, want traj:5", got)
	}
	if got := makeStepID("", 5); got != "5" {
		t.Errorf("makeStepID(empty traj) = %q, want 5", got)
	}
}

func TestStepFromUpdateTextResponse(t *testing.T) {
	su := pb.StepUpdate_builder{
		TrajectoryId: proto.String("t1"),
		StepIndex:    proto.Uint32(3),
		State:        pb.StepUpdate_STATE_DONE.Enum(),
		Source:       pb.StepUpdate_SOURCE_MODEL.Enum(),
		Target:       pb.StepUpdate_TARGET_USER.Enum(),
		Text:         proto.String("hello"),
		CascadeId:    proto.String("t1"),
	}.Build()

	step, err := stepFromUpdate(su, nil)
	if err != nil {
		t.Fatal(err)
	}
	if step.ID != "t1:3" || step.StepIndex != 3 {
		t.Errorf("id/index = %q/%d, want t1:3/3", step.ID, step.StepIndex)
	}
	if step.Type != agtypes.StepTypeTextResponse {
		t.Errorf("type = %v, want TEXT_RESPONSE", step.Type)
	}
	if step.Source != agtypes.StepSourceModel || step.Target != agtypes.StepTargetUser {
		t.Errorf("source/target = %v/%v", step.Source, step.Target)
	}
	if step.IsCompleteResponse == nil || !*step.IsCompleteResponse {
		t.Error("expected IsCompleteResponse=true for done model text to user")
	}
	if step.Extra[ExtraCascadeID] != "t1" || step.Extra[ExtraTrajectoryID] != "t1" {
		t.Errorf("extra = %v", step.Extra)
	}
}

func TestStepFromUpdateFileToolCanonicalPath(t *testing.T) {
	su := pb.StepUpdate_builder{
		TrajectoryId: proto.String("t"),
		StepIndex:    proto.Uint32(1),
		State:        pb.StepUpdate_STATE_ACTIVE.Enum(),
		ViewFile: pb.ActionViewFile_builder{
			FilePath: proto.String("file:///ws/sub/f.txt"),
		}.Build(),
	}.Build()

	step, err := stepFromUpdate(su, nil)
	if err != nil {
		t.Fatal(err)
	}
	if step.Type != agtypes.StepTypeToolCall {
		t.Fatalf("type = %v, want TOOL_CALL", step.Type)
	}
	if len(step.ToolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(step.ToolCalls))
	}
	tc := step.ToolCalls[0]
	if tc.Name != string(agtypes.ToolViewFile) {
		t.Errorf("tool name = %q, want view_file", tc.Name)
	}
	// The file:// path must be normalized and surfaced as the canonical path.
	if tc.CanonicalPath != "/ws/sub/f.txt" {
		t.Errorf("canonical path = %q, want /ws/sub/f.txt", tc.CanonicalPath)
	}
	if tc.Args["file_path"] != "/ws/sub/f.txt" {
		t.Errorf("args[file_path] = %v, want normalized path", tc.Args["file_path"])
	}
}

func TestStepFromUpdateFinishStructuredOutput(t *testing.T) {
	su := pb.StepUpdate_builder{
		TrajectoryId: proto.String("t"),
		StepIndex:    proto.Uint32(9),
		State:        pb.StepUpdate_STATE_DONE.Enum(),
		Finish: pb.ActionFinish_builder{
			OutputString: proto.String(`{"answer":42}`),
		}.Build(),
	}.Build()

	step, err := stepFromUpdate(su, nil)
	if err != nil {
		t.Fatal(err)
	}
	if step.Type != agtypes.StepTypeFinish {
		t.Fatalf("type = %v, want FINISH", step.Type)
	}
	m, ok := step.StructuredOutput.(map[string]any)
	if !ok {
		t.Fatalf("structured output type = %T, want map", step.StructuredOutput)
	}
	if m["answer"] != float64(42) {
		t.Errorf("structured output = %v, want answer=42", m)
	}
}

func TestExtractToolResult(t *testing.T) {
	tests := map[string]struct {
		su   *pb.StepUpdate
		want ToolOutput
	}{
		"run_command": {
			su:   pb.StepUpdate_builder{RunCommand: pb.ActionRunCommand_builder{CombinedOutput: proto.String("hi\n")}.Build()}.Build(),
			want: RunCommandResult{Output: "hi\n"},
		},
		"search_directory": {
			su:   pb.StepUpdate_builder{SearchDirectory: pb.ActionSearchDirectory_builder{NumResults: proto.Int32(3)}.Build()}.Build(),
			want: SearchDirectoryResult{NumResults: 3},
		},
		"find_file": {
			su:   pb.StepUpdate_builder{FindFile: pb.ActionFindFile_builder{Output: proto.String("/a\n/b")}.Build()}.Build(),
			want: FindFileResult{Output: "/a\n/b"},
		},
		"generate_image": {
			su:   pb.StepUpdate_builder{GenerateImage: pb.ActionGenerateImage_builder{ImageName: proto.String("sunset")}.Build()}.Build(),
			want: GenerateImageResult{ImageName: "sunset"},
		},
		"list_directory": {
			su: pb.StepUpdate_builder{ListDirectory: pb.ActionListDirectory_builder{
				Results: []*pb.ActionListDirectory_Result{
					pb.ActionListDirectory_Result_builder{Name: proto.String("a.txt"), FileSize: proto.Uint64(10)}.Build(),
				},
			}.Build()}.Build(),
			want: ListDirectoryResult{Entries: []ListDirectoryEntry{{Name: "a.txt", FileSize: 10}}},
		},
		"no action": {
			su:   pb.StepUpdate_builder{Text: proto.String("just text")}.Build(),
			want: nil,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := extractToolResult(tc.su)
			if tc.want == nil {
				if got != nil {
					t.Errorf("extractToolResult = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("extractToolResult = nil, want %v", tc.want)
			}
			if got.String() != tc.want.String() {
				t.Errorf("extractToolResult String = %q, want %q", got.String(), tc.want.String())
			}
		})
	}
}

func TestStepTracker(t *testing.T) {
	tr := newStepTracker()
	tr.updateState(pb.StepUpdate_STATE_WAITING_FOR_USER)
	if !tr.markHandled("questions_request") {
		t.Error("first markHandled should be true")
	}
	if tr.markHandled("questions_request") {
		t.Error("second markHandled (same state) should be false")
	}
	// Leaving the waiting state clears handled requests.
	tr.updateState(pb.StepUpdate_STATE_ACTIVE)
	tr.updateState(pb.StepUpdate_STATE_WAITING_FOR_USER)
	if !tr.markHandled("questions_request") {
		t.Error("markHandled after re-entering wait should be true again")
	}
}

func TestParseUsageMetadata(t *testing.T) {
	u := pb.UsageMetadata_builder{
		PromptTokenCount: proto.Int32(10),
		TotalTokenCount:  proto.Int32(25),
	}.Build()
	got := parseUsageMetadata(u)
	if got.PromptTokenCount == nil || *got.PromptTokenCount != 10 {
		t.Errorf("prompt = %v, want 10", got.PromptTokenCount)
	}
	// An unset field stays nil (present/absent distinction preserved).
	if got.CandidatesTokenCount != nil {
		t.Errorf("candidates = %v, want nil (unset)", got.CandidatesTokenCount)
	}
}
