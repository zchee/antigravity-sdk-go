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

// Package policy provides a declarative tool-call policy system enforced
// through the hook layer.
//
// Policies express APPROVE, DENY, or ASK_USER decisions for named tools (or the
// "*" wildcard) and are evaluated by a priority model where specificity and
// safety determine precedence:
//
//	specific deny > specific ask > specific allow >
//	wildcard deny > wildcard ask > wildcard allow
//
// Within a priority group the first matching policy wins, so evaluation
// short-circuits. Enforce compiles a set of policies into a
// hook.PreToolCallDecide ready to register with a hook.Runner.
//
// Policy denial vs. disabling tools: a policy-denied tool remains visible to
// the model (the call is rejected at runtime), whereas
// agtypes.CapabilitiesConfig.DisabledTools removes the tool from the model's
// context entirely. Use policies for conditional restrictions; disable tools
// that are simply irrelevant.
package policy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	"github.com/zchee/antigravity-sdk-go/hook"
)

// wildcard is the tool selector that matches every tool.
const wildcard = "*"

// Predicate reports whether a policy applies to a given tool call. It receives
// the full ToolCall (read tc.Args for arguments) and may perform I/O, so it
// takes a context and may return an error. A nil Predicate on a Policy means
// the policy matches any call to its tool.
//
// Upstream inspects the predicate's first parameter annotation to decide
// whether to pass the args dict, a typed model, or the ToolCall. Go has no
// such reflection; the predicate always receives the ToolCall and reads what it
// needs. This is a deliberate, documented simplification.
type Predicate func(ctx context.Context, tc agtypes.ToolCall) (bool, error)

// AskUserHandler obtains user approval for a tool call. It returns true to
// approve execution. Like Predicate it takes a context and may return an error.
type AskUserHandler func(ctx context.Context, tc agtypes.ToolCall) (bool, error)

// Decision is the outcome a policy can produce.
type Decision int

// Policy decisions.
const (
	Approve Decision = iota
	Deny
	AskUser
)

// Policy is a single tool-call rule. Construct policies with the builder
// functions (Allow, DenyTool, Ask) rather than by hand.
type Policy struct {
	// Tool is the tool name this policy targets, or "*" for all tools.
	Tool string
	// Decision is the outcome when this policy matches.
	Decision Decision
	// When is an optional predicate on the tool call. When nil, the policy
	// matches any call to Tool.
	When Predicate
	// AskUser is the handler invoked when Decision is AskUser. It must be set
	// for AskUser policies (validated by Enforce).
	AskUser AskUserHandler
	// Name is a human-readable label used in logging and deny messages.
	Name string
}

// Allow returns an Approve policy for tool. A nil when matches any call.
func Allow(tool string, when Predicate, name string) Policy {
	return Policy{Tool: tool, Decision: Approve, When: when, Name: name}
}

// DenyTool returns a Deny policy for tool. A nil when matches any call.
//
// It is named DenyTool rather than Deny to avoid colliding with the Deny
// Decision constant.
func DenyTool(tool string, when Predicate, name string) Policy {
	return Policy{Tool: tool, Decision: Deny, When: when, Name: name}
}

// Ask returns an AskUser policy for tool, invoking handler to obtain approval.
// A nil when matches any call.
func Ask(tool string, handler AskUserHandler, when Predicate, name string) Policy {
	return Policy{Tool: tool, Decision: AskUser, When: when, AskUser: handler, Name: name}
}

// AllowAll returns a policy that approves every tool call without confirmation.
// Equivalent to Allow("*", nil, "allow_all").
func AllowAll() Policy {
	return Allow(wildcard, nil, "allow_all")
}

// DenyAll returns a policy that denies every tool call. Use it as a base rule
// with specific Allow overrides for a deny-by-default posture: because specific
// policies outrank wildcard ones, []Policy{DenyAll(), Allow("view_file", ...)}
// allows only view_file.
func DenyAll() Policy {
	return DenyTool(wildcard, nil, "deny_all")
}

// SafeDefaults returns policies that allow all read-only tools and ask the user
// for any other tool call.
func SafeDefaults(handler AskUserHandler) []Policy {
	policies := make([]Policy, 0, len(agtypes.ReadOnlyTools())+1)
	for _, t := range agtypes.ReadOnlyTools() {
		policies = append(policies, Allow(string(t), nil, ""))
	}
	return append(policies, Ask(wildcard, handler, nil, ""))
}

// ConfirmRunCommand returns the default LocalAgentConfig policy: every tool is
// allowed except run_command. When handler is nil, run_command is denied
// outright; when a handler is given, run_command calls trigger an ask-user
// flow. Pass AllowAll explicitly to enable autonomous shell access.
func ConfirmRunCommand(handler AskUserHandler) []Policy {
	const name = "confirm_run_command"
	if handler != nil {
		return []Policy{
			Ask(string(agtypes.ToolRunCommand), handler, nil, name),
			Allow(wildcard, nil, name),
		}
	}
	return []Policy{
		DenyTool(string(agtypes.ToolRunCommand), nil, name),
		Allow(wildcard, nil, name),
	}
}

// WorkspaceOnly restricts file tools (view_file, create_file, edit_file) to the
// given workspace directories. A file operation whose canonical path falls
// outside every workspace is denied; other tools are unaffected.
//
// The predicate reads ToolCall.CanonicalPath, which the connection layer
// populates with a normalized path. A file tool call whose CanonicalPath is
// empty is DENIED: a file operation that cannot be proven to target a path
// inside a workspace must not be permitted by a sandbox. The connection layer
// is therefore responsible for populating CanonicalPath for every file tool
// call before this policy can usefully allow anything. (This is why the file
// tools governed here exclude path-less utilities such as a directory listing.)
func WorkspaceOnly(workspaces []string) []Policy {
	outside := func(_ context.Context, tc agtypes.ToolCall) (bool, error) {
		path := tc.CanonicalPath
		if path == "" {
			// A file operation without a resolvable path cannot be shown to be
			// in-workspace; deny it rather than failing open.
			return true, nil
		}
		for _, ws := range workspaces {
			if IsPathInWorkspace(path, ws) {
				return false, nil
			}
		}
		return true, nil
	}
	fileTools := agtypes.FileTools()
	policies := make([]Policy, 0, len(fileTools))
	for _, t := range fileTools {
		policies = append(policies, DenyTool(string(t), outside, "workspace_only"))
	}
	return policies
}

// Priority bucket indices; lower means higher priority.
const (
	levelSpecificDeny = iota
	levelSpecificAsk
	levelSpecificAllow
	levelWildcardDeny
	levelWildcardAsk
	levelWildcardAllow
	numLevels
)

// bucketIndex returns the priority bucket for a policy.
func bucketIndex(p Policy) int {
	specific := p.Tool != wildcard
	switch p.Decision {
	case Deny:
		if specific {
			return levelSpecificDeny
		}
		return levelWildcardDeny
	case AskUser:
		if specific {
			return levelSpecificAsk
		}
		return levelWildcardAsk
	default: // Approve
		if specific {
			return levelSpecificAllow
		}
		return levelWildcardAllow
	}
}

// matchesTool reports whether the policy's selector matches toolName.
func matchesTool(p Policy, toolName string) bool {
	return p.Tool == wildcard || p.Tool == toolName
}

// ErrMissingAskUserHandler reports an AskUser policy compiled without a handler.
var ErrMissingAskUserHandler = errors.New("policy: ASK_USER policy is missing an ask_user handler")

// Enforce compiles policies into a hook.PreToolCallDecide. It validates that
// every AskUser policy has a handler, returning an error joined with
// ErrMissingAskUserHandler otherwise, and pre-sorts policies into priority
// buckets so the returned hook can short-circuit on the first match.
//
// The returned hook fails closed: any panic or error while evaluating a policy
// produces a denial naming the offending policy. When no policy matches, the
// hook allows the call (default open).
func Enforce(policies []Policy) (hook.PreToolCallDecide, error) {
	for _, p := range policies {
		if p.Decision == AskUser && p.AskUser == nil {
			return nil, fmt.Errorf("%w: %q", ErrMissingAskUserHandler, label(p))
		}
	}

	buckets := make([][]Policy, numLevels)
	for _, p := range policies {
		i := bucketIndex(p)
		buckets[i] = append(buckets[i], p)
	}

	return func(ctx context.Context, _ *hook.Context, call agtypes.ToolCall) (agtypes.HookResult, error) {
		for _, bucket := range buckets {
			for _, p := range bucket {
				res, matched := evaluatePolicy(ctx, p, call)
				if matched {
					return res, nil
				}
			}
		}
		return agtypes.AllowHookResult(), nil
	}, nil
}

// evaluatePolicy evaluates a single policy against the tool call. The second
// result reports whether the policy matched and thus decided the outcome. It
// fails closed: a predicate or handler error, or a panic, yields a denying
// matched result naming the policy.
func evaluatePolicy(ctx context.Context, p Policy, call agtypes.ToolCall) (res agtypes.HookResult, matched bool) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic during policy evaluation; failing closed", "policy", label(p), "recover", r)
			res = agtypes.HookResult{Allow: false, Message: fmt.Sprintf("Policy evaluation failed for policy %q: %v", label(p), r)}
			matched = true
		}
	}()

	if !matchesTool(p, call.Name) {
		return agtypes.HookResult{}, false
	}
	if p.When != nil {
		ok, err := p.When(ctx, call)
		if err != nil {
			slog.Error("policy predicate error; failing closed", "policy", label(p), "err", err)
			return agtypes.HookResult{Allow: false, Message: fmt.Sprintf("Policy evaluation failed for policy %q: %v", label(p), err)}, true
		}
		if !ok {
			return agtypes.HookResult{}, false
		}
	}
	return apply(ctx, p, call), true
}

// apply produces the HookResult for a matched policy.
func apply(ctx context.Context, p Policy, call agtypes.ToolCall) agtypes.HookResult {
	l := label(p)
	switch p.Decision {
	case Deny:
		slog.Info("policy denied tool", "policy", l, "tool", call.Name)
		return agtypes.HookResult{Allow: false, Message: fmt.Sprintf("Denied by policy %q.", l)}
	case Approve:
		slog.Info("policy approved tool", "policy", l, "tool", call.Name)
		return agtypes.AllowHookResult()
	default: // AskUser
		slog.Info("policy requesting user approval", "policy", l, "tool", call.Name)
		approved, err := p.AskUser(ctx, call)
		if err != nil {
			return agtypes.HookResult{Allow: false, Message: fmt.Sprintf("Policy evaluation failed for policy %q: %v", l, err)}
		}
		if approved {
			return agtypes.AllowHookResult()
		}
		return agtypes.HookResult{Allow: false, Message: fmt.Sprintf("User denied tool %q (policy %q).", call.Name, l)}
	}
}

// label returns the human-readable identifier for a policy: its Name if set,
// otherwise its Tool selector.
func label(p Policy) string {
	if p.Name != "" {
		return p.Name
	}
	return p.Tool
}
