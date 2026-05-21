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

// helloworld is the runnable form of the README quick-start example.
// Requires the localharness binary on PATH (or ANTIGRAVITY_HARNESS_PATH) and a
// GEMINI_API_KEY environment variable.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/zchee/antigravity-sdk-go"
)

func main() {
	ctx := context.Background()

	// The default config allows all builtin tools except run_command, which
	// must be explicitly confirmed (see policy.ConfirmRunCommand). File tools
	// are auto-scoped to the current working directory.
	cfg := &antigravity.LocalAgentConfig{
		APIKey: os.Getenv("GEMINI_API_KEY"),
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
