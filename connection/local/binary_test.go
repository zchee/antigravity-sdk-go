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
	"errors"
	"testing"
)

// TestStrategyResolveHarnessProviderBeatsAll verifies that, when set,
// HarnessProvider takes precedence over HarnessPath and the env var.
func TestStrategyResolveHarnessProviderBeatsAll(t *testing.T) {
	t.Setenv(HarnessPathEnv, "/from/env")
	called := false
	s := NewStrategy(StrategyConfig{
		HarnessPath: "/from/field",
		HarnessProvider: func(context.Context) (string, func(), error) {
			called = true
			return "/from/provider", nil, nil
		},
	})
	got, err := s.resolveHarness(t.Context())
	if err != nil {
		t.Fatalf("resolveHarness: %v", err)
	}
	if !called {
		t.Error("provider was not invoked")
	}
	if got != "/from/provider" {
		t.Errorf("path = %q, want /from/provider", got)
	}
}

// TestStrategyResolveHarnessProviderCleanupRunsOnClose verifies the provider's
// cleanup is invoked exactly once during Strategy.Close, even when no
// connection was established.
func TestStrategyResolveHarnessProviderCleanupRunsOnClose(t *testing.T) {
	var cleanups int
	s := NewStrategy(StrategyConfig{
		HarnessProvider: func(context.Context) (string, func(), error) {
			return "/tmp/harness", func() { cleanups++ }, nil
		},
	})
	if _, err := s.resolveHarness(t.Context()); err != nil {
		t.Fatalf("resolveHarness: %v", err)
	}
	if cleanups != 0 {
		t.Fatalf("cleanup ran before Close (count=%d)", cleanups)
	}
	if err := s.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if cleanups != 1 {
		t.Errorf("cleanup ran %d times, want 1", cleanups)
	}
	// Close is idempotent — second call must not re-run cleanup.
	if err := s.Close(t.Context()); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if cleanups != 1 {
		t.Errorf("cleanup ran %d times after second Close, want 1", cleanups)
	}
}

// TestStrategyResolveHarnessProviderErrorRunsCleanup verifies that a provider
// returning (path, cleanup, err) still has its cleanup invoked, so a
// half-extracted asset is never leaked.
func TestStrategyResolveHarnessProviderErrorRunsCleanup(t *testing.T) {
	var cleanups int
	sentinel := errors.New("extract failed")
	s := NewStrategy(StrategyConfig{
		HarnessProvider: func(context.Context) (string, func(), error) {
			return "", func() { cleanups++ }, sentinel
		},
	})
	_, err := s.resolveHarness(t.Context())
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want wrapping %v", err, sentinel)
	}
	if cleanups != 1 {
		t.Errorf("cleanup ran %d times after provider error, want 1", cleanups)
	}
}

// TestStrategyResolveHarnessProviderEmptyPath verifies that a provider that
// returns "" without an error is rejected, and its cleanup still runs.
func TestStrategyResolveHarnessProviderEmptyPath(t *testing.T) {
	var cleanups int
	s := NewStrategy(StrategyConfig{
		HarnessProvider: func(context.Context) (string, func(), error) {
			return "", func() { cleanups++ }, nil
		},
	})
	if _, err := s.resolveHarness(t.Context()); err == nil {
		t.Error("resolveHarness with empty provider path = nil error, want failure")
	}
	if cleanups != 1 {
		t.Errorf("cleanup ran %d times, want 1", cleanups)
	}
}

// TestStrategyResolveHarnessExplicitPath verifies HarnessPath beats the env var
// and PATH when no provider is set.
func TestStrategyResolveHarnessExplicitPath(t *testing.T) {
	t.Setenv(HarnessPathEnv, "/from/env")
	s := NewStrategy(StrategyConfig{HarnessPath: "/from/field"})
	got, err := s.resolveHarness(t.Context())
	if err != nil {
		t.Fatalf("resolveHarness: %v", err)
	}
	if got != "/from/field" {
		t.Errorf("path = %q, want /from/field", got)
	}
}
