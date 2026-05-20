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
	"os"
	"os/exec"
	"testing"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/connection/local"
)

// TestIntegrationHelloWorld exercises the real localharness wire protocol end
// to end. It is skipped unless both a binary (ANTIGRAVITY_HARNESS_PATH or
// "localharness" on PATH) and a Gemini API key (GEMINI_API_KEY) are available.
//
// This is the only test that verifies ground-truth wire fidelity; the other
// tests in this package use synthetic proto fixtures and verify structural
// correctness only. When a binary becomes available in CI, set
// ANTIGRAVITY_HARNESS_PATH so this runs and catches any wire drift.
func TestIntegrationHelloWorld(t *testing.T) {
	if os.Getenv("ANTIGRAVITY_HARNESS_PATH") == "" {
		if _, err := exec.LookPath("localharness"); err != nil {
			t.Skip("localharness binary not available; set ANTIGRAVITY_HARNESS_PATH or put it on PATH to run")
		}
	}
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set; required for the live harness")
	}

	cfg, err := (&local.AgentConfig{}).Build()
	if err != nil {
		t.Fatal(err)
	}
	strategy, err := cfg.CreateStrategy(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err := strategy.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = strategy.Close(ctx) })

	conn, err := strategy.Connect()
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := conn.Send(ctx, "Reply with exactly: pong"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	var sawModelText bool
	for step, err := range conn.ReceiveSteps(ctx) {
		if err != nil {
			t.Fatalf("ReceiveSteps: %v", err)
		}
		if step.Source == agtypes.StepSourceModel && step.Content != "" {
			sawModelText = true
		}
	}
	if !sawModelText {
		t.Error("no model text received from the live harness")
	}
}
