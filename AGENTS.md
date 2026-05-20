# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Go SDK for building AI agents on Google Antigravity and Gemini. It is a **port of
[google-antigravity/antigravity-sdk-python](https://github.com/google-antigravity/antigravity-sdk-python)** —
treat that repo as the source of truth for the public API surface and behavior.

- **Alpha stage**, greenfield: expect to be creating packages, not editing established ones.
- Toolchain is **Go 1.26** (see `go.mod`); modern Go idioms are expected, not optional.

## Porting conventions

- **Restructure into idiomatic Go packages** — do not mirror the Python file/module layout.
  Map Python modules onto Go package conventions while keeping the **public API surface at parity**
  with upstream (same operations, same semantics).
- **Track upstream `main`** — there are no tagged releases yet. When porting, fetch the current
  state of the corresponding Python source rather than relying on memory.
- Use `/port-from-python` to port a Python source file and diff the resulting API surface for parity.

(Go style, formatting, testing, and benchmark conventions are defined globally in
`~/.claude/instructions/Go.md` and apply here — they are not repeated in this file.)

## Commit conventions

- Add a `Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>` trailer to commit messages.
