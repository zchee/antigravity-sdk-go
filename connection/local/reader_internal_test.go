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
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/hook"
	"github.com/zchee/antigravity-sdk-go/hook/policy"
	pb "github.com/zchee/antigravity-sdk-go/internal/localharnesspb"
)

// newTestConn builds a LocalConnection wired with a hook runner but no
// websocket/process, sufficient to exercise the decision logic
// (decideToolConfirmation, buildQuestionAnswers) that does not itself write.
func newTestConn(hr *hook.Runner) *LocalConnection {
	ctx, cancel := context.WithCancel(context.Background())
	return &LocalConnection{
		hookRunner:        hr,
		readerCtx:         ctx,
		readerCancel:      cancel,
		stepTrackers:      make(map[stepKey]*stepTracker),
		pendingBuiltin:    make(map[stepKey]pendingCall),
		activeSubagents:   make(map[string]struct{}),
		subagentResponses: make(map[string]string),
	}
}

func TestDecideToolConfirmationPolicy(t *testing.T) {
	tests := map[string]struct {
		policies  []policy.Policy
		wantAllow bool
		wantPend  bool
	}{
		"deny run_command": {
			policies:  []policy.Policy{policy.DenyTool(string(agtypes.ToolRunCommand), nil, "no-shell"), policy.AllowAll()},
			wantAllow: false,
			wantPend:  false,
		},
		"allow run_command tracks pending": {
			policies:  []policy.Policy{policy.AllowAll()},
			wantAllow: true,
			wantPend:  true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			hr := hook.NewRunner()
			h, err := policy.Enforce(tc.policies)
			if err != nil {
				t.Fatal(err)
			}
			if err := hr.Register(h); err != nil {
				t.Fatal(err)
			}
			c := newTestConn(hr)
			defer c.readerCancel()

			su := pb.StepUpdate_builder{
				TrajectoryId: proto.String("t"),
				StepIndex:    proto.Uint32(2),
				RunCommand:   pb.ActionRunCommand_builder{CommandLine: proto.String("ls")}.Build(),
			}.Build()

			gotTC, allow := c.decideToolConfirmation(su)
			if allow != tc.wantAllow {
				t.Errorf("allow = %v, want %v", allow, tc.wantAllow)
			}
			if gotTC.Name != string(agtypes.ToolRunCommand) {
				t.Errorf("tool name = %q, want run_command", gotTC.Name)
			}
			_, pending := c.pendingBuiltin[stepKey{trajectoryID: "t", stepIndex: 2}]
			if pending != tc.wantPend {
				t.Errorf("pending tracked = %v, want %v", pending, tc.wantPend)
			}
		})
	}
}

func TestDecideToolConfirmationHostToolAutoApproved(t *testing.T) {
	// A pre-request host-tool confirmation (no builtin action set) is
	// auto-approved without consulting policy.
	hr := hook.NewRunner()
	h, _ := policy.Enforce([]policy.Policy{policy.DenyAll()}) // would deny everything
	_ = hr.Register(h)
	c := newTestConn(hr)
	defer c.readerCancel()

	su := pb.StepUpdate_builder{
		TrajectoryId: proto.String("t"),
		StepIndex:    proto.Uint32(1),
		RequestText:  proto.String("host tool incoming"),
	}.Build()

	tc, allow := c.decideToolConfirmation(su)
	if !allow {
		t.Error("host-tool pre-request should be auto-approved despite deny-all policy")
	}
	if tc.Name != defaultHostToolName {
		t.Errorf("tool name = %q, want %q", tc.Name, defaultHostToolName)
	}
}

func TestBuildQuestionAnswers(t *testing.T) {
	hr := hook.NewRunner()
	// An interaction hook that selects option "2" for the question.
	err := hr.Register(hook.OnInteraction(func(_ context.Context, _ *hook.Context, spec agtypes.AskQuestionInteractionSpec) (agtypes.QuestionHookResult, bool, error) {
		if len(spec.Questions) != 1 {
			t.Errorf("spec questions = %d, want 1", len(spec.Questions))
		}
		return agtypes.QuestionHookResult{
			Responses: []agtypes.QuestionResponse{{SelectedOptionIDs: []string{"2"}}},
		}, true, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	c := newTestConn(hr)
	defer c.readerCancel()

	su := pb.StepUpdate_builder{
		TrajectoryId: proto.String("t"),
		StepIndex:    proto.Uint32(1),
		QuestionsRequest: pb.UserQuestionsRequest_builder{
			Questions: []*pb.UserQuestion{
				pb.UserQuestion_builder{
					MultipleChoice: pb.MultipleChoice_builder{
						Question: proto.String("Pick one"),
						Choices:  []string{"a", "b", "c"},
					}.Build(),
				}.Build(),
			},
		}.Build(),
	}.Build()

	answers, err := c.buildQuestionAnswers(su)
	if err != nil {
		t.Fatal(err)
	}
	if len(answers) != 1 {
		t.Fatalf("got %d answers, want 1", len(answers))
	}
	mc := answers[0].GetMultipleChoiceAnswer()
	if mc == nil {
		t.Fatal("answer missing multiple_choice_answer")
	}
	// Option id "2" maps to zero-based index 1.
	idx := mc.GetSelectedChoiceIndices()
	if len(idx) != 1 || idx[0] != 1 {
		t.Errorf("selected indices = %v, want [1]", idx)
	}
}
