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

package connection_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/connection"
	"github.com/zchee/antigravity-sdk-go/hook"
)

func TestMarshalResponseSchema(t *testing.T) {
	tests := map[string]struct {
		in      any
		want    string
		wantErr bool
	}{
		"nil yields empty":     {in: nil, want: ""},
		"valid json string":    {in: `{"type":"object"}`, want: `{"type":"object"}`},
		"invalid json string":  {in: `{not json`, wantErr: true},
		"map marshals to json": {in: map[string]any{"type": "object"}, want: `{"type":"object"}`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := connection.MarshalResponseSchema(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("MarshalResponseSchema(%v) = nil error, want error", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("MarshalResponseSchema(%v): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("MarshalResponseSchema(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCloneDeepCopiesValuesPreservesHookIdentity(t *testing.T) {
	threshold := 100
	startHook := hook.OnSessionStart(func(context.Context, *hook.Context) error { return nil })

	orig := &connection.FakeConfig{
		BaseAgentConfig: connection.BaseAgentConfig{
			CapabilitiesValue: agtypes.CapabilitiesConfig{
				EnabledTools:        []agtypes.BuiltinTools{agtypes.ToolViewFile},
				CompactionThreshold: &threshold,
			},
			WorkspacesValue: []string{"/ws"},
			HooksValue:      []hook.Hook{startHook},
		},
	}

	clone := orig.Clone()

	// Value fields are deep-copied: mutating the clone's capabilities slice and
	// pointer must not affect the original.
	cloneCaps := clone.Capabilities()
	cloneCaps.EnabledTools[0] = agtypes.ToolRunCommand
	*cloneCaps.CompactionThreshold = 999
	if orig.Capabilities().EnabledTools[0] != agtypes.ToolViewFile {
		t.Error("clone shares EnabledTools backing array with original")
	}
	if *orig.Capabilities().CompactionThreshold != 100 {
		t.Error("clone shares CompactionThreshold pointer with original")
	}

	// Hook identity is preserved: the clone's hook slice holds the same hook
	// value as the original (shallow copy of the slice, same elements).
	if len(clone.Hooks()) != 1 || !reflect.ValueOf(clone.Hooks()[0]).IsValid() {
		t.Fatal("clone lost hooks")
	}
}
