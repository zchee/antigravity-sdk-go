# antigravity-sdk-go

The Google Antigravity SDK is a Go SDK for building AI agents powered by Antigravity and Gemini.

antigravity-sdk-go is a Go port of [google-antigravity/antigravity-sdk-python](https://github.com/google-antigravity/antigravity-sdk-python).

> [!IMPORTANT]
> This project is in the alpha stage.

## Installation

```bash
go get github.com/zchee/antigravity-sdk-go@latest
```

You also need the upstream `localharness` binary on `PATH` (or pointed to via
`ANTIGRAVITY_HARNESS_PATH`) and a `GEMINI_API_KEY`.

## Quick start

```go
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
```

For an interactive REPL with y/n confirmation of `run_command`, see the
[`interactive`](./interactive/) package and `interactive.WithUserConfirmation`.

## Package layout

| Package                      | Role                                                    |
|------------------------------|---------------------------------------------------------|
| `antigravity` (root)         | `Agent`, `Conversation`, `ChatResponse`, public type aliases |
| `agtypes`                    | SDK boundary types (Pydantic models → Go structs)       |
| `connection` / `connection/local` | Backend transport — interfaces and the localharness implementation |
| `hook` / `hook/policy`       | Lifecycle hooks and tool-call policies                  |
| `tool`                       | Host tool runner and `ToolContext`                      |
| `trigger`                    | Long-lived event sources (intervals, file watches)      |
| `mcp`                        | MCP server bridge                                       |
| `interactive`                | Stdin-driven dev REPL helpers                           |
| `internal/localharnesspb`    | Generated proto bindings for the harness wire protocol  |

## Parity with the Python SDK

See [PARITY.md](./PARITY.md) for the full symbol-by-symbol mapping against
upstream `google-antigravity/antigravity-sdk-python` and the rationale for
every deliberate gap or rename.

## Tests

```bash
go test ./...                    # all unit tests; no binary required
go test -race -count=3 ./...     # the bar this SDK is held to
```

A live integration test (`connection/local/integration_test.go`) drives the
real harness over the websocket end to end. It is **skipped automatically**
when the binary or `GEMINI_API_KEY` is missing. To run it:

```bash
ANTIGRAVITY_HARNESS_PATH=/path/to/localharness GEMINI_API_KEY=… \
  go test -run TestIntegration ./connection/local/
```
