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

// Command embeddedharness demonstrates how a downstream Go program can ship
// the localharness binary inside its own Go binary using //go:embed, by wiring
// AgentConfig.HarnessProvider to an extraction routine.
//
// The bin/localharness file checked in here is a small placeholder so the
// example builds without committing a multi-megabyte binary. Replace it with
// a real platform-specific localharness before running. See bin/README.md for
// the platform layout pattern, and PARITY.md §12 for the rationale.
//
// Requires a GEMINI_API_KEY environment variable.
package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"

	"github.com/zchee/antigravity-sdk-go"
	"github.com/zchee/antigravity-sdk-go/connection/local"
)

//go:embed bin/localharness
var harnessBin []byte

// extractHarness writes the embedded harness bytes to a private tempfile and
// returns its path plus a cleanup that removes the tempfile.
func extractHarness(_ context.Context) (string, func(), error) {
	f, err := os.CreateTemp("", "localharness-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	if _, err := f.Write(harnessBin); err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := os.Chmod(f.Name(), 0o700); err != nil {
		cleanup()
		return "", nil, err
	}
	return f.Name(), cleanup, nil
}

func main() {
	ctx := context.Background()

	cfg := &antigravity.LocalAgentConfig{
		APIKey:          os.Getenv("GEMINI_API_KEY"),
		HarnessProvider: local.HarnessProvider(extractHarness),
	}

	agent, err := antigravity.New(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer agent.Close(ctx)

	resp, err := agent.Chat(ctx, "Reply with exactly: pong")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Close()

	text, err := resp.Text()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(text)
}
