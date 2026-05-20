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

package policy

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// errTraversal reports a target path containing a ".." segment, which is
// rejected outright (see IsPathInWorkspace).
var errTraversal = errors.New("policy: target path contains a parent-directory (..) segment")

// IsPathInWorkspace reports whether targetPath lies within workspacePath after
// both are canonicalized (made absolute and symlink-resolved).
//
// It fails closed: if either path cannot be canonicalized — for example a
// workspace that does not exist, or a target whose existing ancestor cannot be
// resolved — it returns false. Containment is computed structurally via
// filepath.Rel, so a sibling whose name shares a prefix with the workspace
// (e.g. "/ws2" against workspace "/ws") is correctly reported as outside.
//
// A target containing any ".." segment is rejected outright (treated as
// outside). This is required for correctness, not just strictness: filepath.Abs
// applies a lexical Clean that would cancel a "symlink/.." pair before symlink
// resolution, so a path like "<ws>/link/../escapee" would appear inside the
// workspace lexically while the kernel resolves "link" first and lands outside.
// Legitimate in-workspace operations never need "..", so rejecting it closes
// the bypass without losing functionality.
//
// Security note: like any policy evaluated before tool execution, this check is
// subject to a time-of-check/time-of-use race — a path validated here could be
// replaced by a symlink before the tool acts on it. This limitation is shared
// with the upstream SDK and is not defended against here.
func IsPathInWorkspace(targetPath, workspacePath string) bool {
	target, err := canonicalizeTarget(targetPath)
	if err != nil {
		return false
	}
	// The workspace must exist; resolve it strictly and fail closed otherwise.
	ws, err := canonicalizeExisting(workspacePath)
	if err != nil {
		return false
	}
	return contains(ws, target)
}

// canonicalizeExisting returns the absolute, symlink-resolved form of path,
// which must exist. It returns an error if the path cannot be resolved.
func canonicalizeExisting(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// canonicalizeTarget returns the absolute, symlink-resolved form of a target
// path that may not yet exist (e.g. a file about to be created).
//
// It resolves the longest existing ancestor with filepath.EvalSymlinks, then
// re-appends the cleaned non-existent tail. This mirrors the upstream
// resolve(strict=False) behavior: existing symlinks in the path are resolved,
// while the not-yet-created suffix is preserved. It returns an error only if
// the existing ancestor cannot be resolved.
func canonicalizeTarget(path string) (string, error) {
	// Reject ".." on the RAW input, before filepath.Abs/Clean can lexically
	// cancel a "symlink/.." pair and hide a traversal from symlink resolution.
	if containsDotDot(path) {
		return "", errTraversal
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	// Find the longest existing ancestor.
	existing := abs
	var tail []string
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			// Reached the root without finding an existing ancestor; resolve
			// against the root itself.
			break
		}
		tail = append([]string{filepath.Base(existing)}, tail...)
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	if len(tail) == 0 {
		return resolved, nil
	}
	return filepath.Join(append([]string{resolved}, tail...)...), nil
}

// containsDotDot reports whether path has any ".." path segment, inspecting the
// raw (pre-Clean) path. It checks both the slash-normalized form and the
// platform separator form so that a "\" separator on Windows is also caught.
func containsDotDot(path string) bool {
	for _, p := range []string{filepath.ToSlash(path), path} {
		for seg := range strings.SplitSeq(p, "/") {
			if seg == ".." {
				return true
			}
		}
	}
	return false
}

// contains reports whether target is ws or lies beneath it. Both arguments must
// already be absolute and cleaned.
func contains(ws, target string) bool {
	rel, err := filepath.Rel(ws, target)
	if err != nil {
		return false
	}
	if caseInsensitiveFS() {
		rel = strings.ToLower(rel)
	}
	if rel == "." {
		return true
	}
	// Outside if the relative path needs to climb above the workspace.
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// caseInsensitiveFS reports whether the host filesystem should be treated as
// case-insensitive for path comparison.
//
// This is a deliberate simplification of the upstream per-path probe: it keys
// off the operating system (macOS and Windows default to case-insensitive
// filesystems) rather than stat-probing each path. On the rare case-sensitive
// volume on those platforms the check errs toward over-restriction (denying a
// genuinely distinct path), which is the safe direction for a security policy.
func caseInsensitiveFS() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}
