---
name: port-from-python
description: Port a source file from the upstream google-antigravity/antigravity-sdk-python SDK into idiomatic Go, then diff the public API surface for parity. Use when adding or updating functionality that mirrors the Python SDK.
---

# Port from the Python SDK

This repo is a Go port of `google-antigravity/antigravity-sdk-python`. Use this workflow to port
a piece of upstream functionality into idiomatic Go while keeping the public API at parity.

## Inputs

`$ARGUMENTS` is the upstream Python path or symbol to port (e.g. `agents/runner.py` or `Runner`).
If empty, ask which upstream module to port.

## Steps

1. **Fetch the current upstream source.** Track `main` (no releases yet). Use the GitHub API so you
   read the live file, not a guess:
   - `gh api repos/google-antigravity/antigravity-sdk-python/contents/<path>?ref=main --jq .content | base64 -d`
   - If you only have a symbol name, find the file first (code search requires auth):
     `gh api -X GET search/code -f q='<symbol> repo:google-antigravity/antigravity-sdk-python' --jq '.items[].path'`
     Fallback if search is unavailable: list the tree and grep —
     `gh api repos/google-antigravity/antigravity-sdk-python/git/trees/main?recursive=1 --jq '.tree[].path' | grep -i <symbol>`
   - Record the upstream commit you ported against: `gh api repos/google-antigravity/antigravity-sdk-python/commits/main --jq .sha`

2. **Port into idiomatic Go** — do NOT mirror the Python file layout. Map the Python module onto a
   sensible Go package. Preserve the **public API surface** (same operations and semantics), but use
   Go conventions for naming, error handling, and types. Follow all rules in
   `~/.claude/instructions/Go.md` (formatting, `any` over `interface{}`, generics, json-experiment,
   table-driven `map[string]struct{...}` tests, `b.Loop()`, etc.).

3. **Diff the API surface for parity.** List the exported Python symbols (classes, functions,
   constants) from the source and confirm each has a Go counterpart, or note deliberate omissions
   with a reason. Use `gopls` (MCP) to inspect the resulting Go package API.

4. **Verify.** `go build ./...` and `go test ./...` must pass. Write table-driven tests per the
   global Go conventions. Run `modernize -fix -test ./...` and confirm formatting is clean.

5. **Report** the upstream commit SHA ported against, the Go package(s) created/changed, and any
   API-parity gaps.
