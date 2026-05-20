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

// Package interactive provides stdin-based utilities for running agents in a
// terminal: hooks that prompt the user to confirm tool calls or answer
// questions, and a REPL loop. These are intended for local development and
// debugging, not production use.
package interactive

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	ag "github.com/zchee/antigravity-sdk-go"
	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/hook"
	"github.com/zchee/antigravity-sdk-go/hook/policy"
)

// ErrInputClosed reports that the input stream reached EOF (the user pressed
// Ctrl-D or the stream ended).
var ErrInputClosed = errors.New("interactive: input closed")

// Prompter reads a single line of user input after displaying a prompt. It
// abstracts stdin so the hooks and loop are testable.
type Prompter interface {
	// Prompt displays prompt and returns the user's line (without the trailing
	// newline). It returns ErrInputClosed at end of input, or ctx.Err() if ctx
	// is cancelled.
	Prompt(ctx context.Context, prompt string) (string, error)
}

// StdinPrompter reads lines from an io.Reader (os.Stdin by default) and writes
// prompts to an io.Writer (os.Stdout by default).
type StdinPrompter struct {
	scanner *bufio.Scanner
	out     io.Writer
}

// NewStdinPrompter returns a Prompter reading os.Stdin and writing os.Stdout.
func NewStdinPrompter() *StdinPrompter {
	return NewPrompter(os.Stdin, os.Stdout)
}

// NewPrompter returns a Prompter reading lines from r and writing prompts to w.
func NewPrompter(r io.Reader, w io.Writer) *StdinPrompter {
	return &StdinPrompter{scanner: bufio.NewScanner(r), out: w}
}

// Prompt displays prompt and reads one line. The read runs in a goroutine so a
// cancelled context returns promptly; the goroutine may remain blocked on the
// underlying reader until the next line or EOF, which is acceptable for a
// development REPL that exits with the process.
func (p *StdinPrompter) Prompt(ctx context.Context, prompt string) (string, error) {
	if prompt != "" {
		fmt.Fprint(p.out, prompt)
	}
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		if p.scanner.Scan() {
			ch <- result{line: p.scanner.Text()}
			return
		}
		if err := p.scanner.Err(); err != nil {
			ch <- result{err: err}
			return
		}
		ch <- result{err: ErrInputClosed}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		return r.line, r.err
	}
}

// affirmative reports whether s is a yes-style answer.
func affirmative(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// NewToolConfirmationHook returns a pre-tool-call decide hook that asks the user
// (via p) to confirm each tool call. The default on empty or closed input is to
// deny.
func NewToolConfirmationHook(p Prompter) hook.PreToolCallDecide {
	return func(ctx context.Context, _ *hook.Context, call agtypes.ToolCall) (agtypes.HookResult, error) {
		fmt.Printf("\nTool execution requested: %s\n", call.Name)
		if len(call.Args) > 0 {
			fmt.Printf("Arguments: %v\n", call.Args)
		}
		ans, err := p.Prompt(ctx, "Allow execution? (y/n) [n]: ")
		if err != nil {
			if errors.Is(err, ErrInputClosed) {
				return agtypes.HookResult{Allow: false, Message: "User denied tool call."}, nil
			}
			return agtypes.HookResult{}, err
		}
		if affirmative(ans) {
			return agtypes.AllowHookResult(), nil
		}
		return agtypes.HookResult{Allow: false, Message: "User denied tool call."}, nil
	}
}

// AskUserHandler returns a policy ask-user handler that prompts the user (via p)
// to confirm a tool call, returning true to allow.
func AskUserHandler(p Prompter) policy.AskUserHandler {
	return func(ctx context.Context, tc agtypes.ToolCall) (bool, error) {
		fmt.Printf("\nPolicy check: Tool execution requested: %s\n", tc.Name)
		if len(tc.Args) > 0 {
			fmt.Printf("Arguments: %v\n", tc.Args)
		}
		ans, err := p.Prompt(ctx, "Allow execution? (y/n) [n]: ")
		if err != nil {
			if errors.Is(err, ErrInputClosed) {
				return false, nil
			}
			return false, err
		}
		return affirmative(ans), nil
	}
}

// NewAskQuestionHook returns an interaction hook that prompts the user (via p)
// to answer each question. A numeric answer selects an option by position; an
// answer matching an option's text or id selects it; an empty answer skips; any
// other answer is recorded as a freeform response. End of input cancels the
// interaction.
func NewAskQuestionHook(p Prompter) hook.OnInteraction {
	return func(ctx context.Context, _ *hook.Context, spec agtypes.AskQuestionInteractionSpec) (agtypes.QuestionHookResult, bool, error) {
		var responses []agtypes.QuestionResponse
		for _, q := range spec.Questions {
			fmt.Printf("\nQuestion: %s\n", q.Question)
			for i, opt := range q.Options {
				fmt.Printf("  %d. %s\n", i+1, opt.Text)
			}
			ans, err := p.Prompt(ctx, "Response: ")
			if err != nil {
				if errors.Is(err, ErrInputClosed) {
					return agtypes.QuestionHookResult{Responses: responses, Cancelled: true}, true, nil
				}
				return agtypes.QuestionHookResult{}, false, err
			}
			responses = append(responses, answerToResponse(strings.TrimSpace(ans), q.Options))
		}
		return agtypes.QuestionHookResult{Responses: responses}, true, nil
	}
}

// answerToResponse maps a trimmed user answer to a QuestionResponse against the
// question's options.
func answerToResponse(ans string, options []agtypes.AskQuestionOption) agtypes.QuestionResponse {
	if ans == "" {
		return agtypes.QuestionResponse{Skipped: true}
	}
	if id := matchOption(ans, options); id != "" {
		return agtypes.QuestionResponse{SelectedOptionIDs: []string{id}}
	}
	return agtypes.QuestionResponse{FreeformResponse: ans}
}

// matchOption returns the id of the option matching ans (by 1-based number,
// then by case-insensitive text or id), or "" if none matches.
func matchOption(ans string, options []agtypes.AskQuestionOption) string {
	if len(options) == 0 {
		return ""
	}
	if n, err := strconv.Atoi(ans); err == nil {
		if n >= 1 && n <= len(options) {
			return options[n-1].ID
		}
	}
	for _, opt := range options {
		if strings.EqualFold(ans, opt.Text) || strings.EqualFold(ans, opt.ID) {
			return opt.ID
		}
	}
	return ""
}

// WithUserConfirmation returns cfg with any bare deny-run_command policy
// replaced by an ask-user policy wired to AskUserHandler(p), giving interactive
// users a y/n prompt instead of a hard denial. Other policies are unchanged.
//
// This is the Go-clean replacement for the upstream's
// _upgrade_to_interactive_confirmation, which mutated a started agent's private
// state: opt in before constructing the Agent, e.g.
//
//	cfg := interactive.WithUserConfirmation(&antigravity.LocalAgentConfig{...}, p)
//	agent, err := antigravity.New(ctx, cfg)
func WithUserConfirmation(cfg ag.AgentConfig, p Prompter) ag.AgentConfig {
	policies := cfg.Policies()
	upgraded := make([]policy.Policy, 0, len(policies))
	for _, pol := range policies {
		if pol.Tool == string(agtypes.ToolRunCommand) && pol.Decision == policy.Deny && pol.When == nil {
			name := pol.Name
			if name == "" {
				name = "interactive_confirm"
			}
			upgraded = append(upgraded, policy.Ask(string(agtypes.ToolRunCommand), AskUserHandler(p), nil, name))
			continue
		}
		upgraded = append(upgraded, pol)
	}
	cfg.SetPolicies(upgraded)
	return cfg
}

// RunInteractiveLoop runs a REPL against a started agent: it registers an
// ask-question hook, then reads lines from p, sends each as a chat turn, and
// prints the agent's streamed text. Typing "exit" or "quit" (or end of input)
// ends the loop.
//
// For y/n confirmation of run_command instead of hard denial, construct the
// agent's config with WithUserConfirmation before starting.
func RunInteractiveLoop(ctx context.Context, agent *ag.Agent, p Prompter) error {
	if !agent.IsStarted() {
		return ag.ErrAgentNotStarted
	}
	if err := agent.RegisterHook(NewAskQuestionHook(p)); err != nil {
		return err
	}

	fmt.Println("Starting interactive loop. Type 'exit' or 'quit' to end.")
	for {
		input, err := p.Prompt(ctx, "User: ")
		if err != nil {
			if errors.Is(err, ErrInputClosed) || errors.Is(err, context.Canceled) {
				fmt.Println("\nGoodbye!")
				return nil
			}
			return err
		}
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		if l := strings.ToLower(input); l == "exit" || l == "quit" {
			fmt.Println("Goodbye!")
			return nil
		}

		resp, err := agent.Chat(ctx, input)
		if err != nil {
			return err
		}
		fmt.Print("Agent: ")
		for delta, err := range resp.TextDeltas() {
			if err != nil {
				resp.Close()
				return err
			}
			fmt.Print(delta)
		}
		fmt.Println()
		resp.Close()
	}
}
