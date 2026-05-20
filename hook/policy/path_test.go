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

package policy_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zchee/antigravity-sdk-go/hook/policy"
)

func TestIsPathInWorkspace(t *testing.T) {
	ws := t.TempDir()
	// Resolve the workspace itself (t.TempDir may sit under a symlinked /tmp on
	// macOS); tests build target paths from the unresolved ws to exercise
	// canonicalization.
	if err := os.MkdirAll(filepath.Join(ws, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "sub", "f.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Sibling directory sharing a name prefix with ws.
	siblingPrefix := ws + "2"
	if err := os.MkdirAll(siblingPrefix, 0o755); err != nil {
		t.Fatal(err)
	}

	tests := map[string]struct {
		target string
		want   bool
	}{
		"workspace root itself":         {target: ws, want: true},
		"existing nested file":          {target: filepath.Join(ws, "sub", "f.txt"), want: true},
		"existing nested dir":           {target: filepath.Join(ws, "sub", "deep"), want: true},
		"non-existent file inside":      {target: filepath.Join(ws, "sub", "new.txt"), want: true},
		"non-existent deep path inside": {target: filepath.Join(ws, "a", "b", "c.txt"), want: true},
		// Filenames that merely contain dots must NOT be mistaken for ".."
		// traversal. These pin the containsDotDot boundary against a future
		// over-broad simplification (e.g. strings.Contains(p, "..")).
		"dotfile inside":              {target: filepath.Join(ws, "sub", ".bashrc"), want: true},
		"double-dot-in-name inside":   {target: filepath.Join(ws, "sub", "a..b"), want: true},
		"triple-dot-name inside":      {target: filepath.Join(ws, "sub", "file...txt"), want: true},
		"parent traversal escapes":    {target: filepath.Join(ws, "..", "etc", "passwd"), want: false},
		"sibling sharing name prefix": {target: filepath.Join(siblingPrefix, "f.txt"), want: false},
		"unrelated absolute path":     {target: filepath.Dir(ws), want: false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := policy.IsPathInWorkspace(tc.target, ws); got != tc.want {
				t.Errorf("IsPathInWorkspace(%q, %q) = %v, want %v", tc.target, ws, got, tc.want)
			}
		})
	}
}

func TestIsPathInWorkspaceSymlinkEscape(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A symlink inside the workspace pointing outside it.
	link := filepath.Join(ws, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	// Existing target reached via the escaping symlink resolves outside.
	if policy.IsPathInWorkspace(filepath.Join(link, "secret"), ws) {
		t.Error("path reached via escaping symlink reported inside workspace")
	}
	// Non-existent target whose existing ancestor (the symlink) escapes is also
	// outside.
	if policy.IsPathInWorkspace(filepath.Join(link, "newfile"), ws) {
		t.Error("non-existent path under escaping symlink reported inside workspace")
	}
}

// TestIsPathInWorkspaceDotDotAfterSymlink covers the lexical-Clean bypass: a
// raw target string of the form "<ws>/<symlink>/../tail". filepath.Abs would
// Clean "<symlink>/.." to nothing before symlink resolution, making the path
// appear inside the workspace while the kernel resolves the symlink first and
// escapes. The ".." segments must be rejected on the raw input.
//
// The inputs are built by string concatenation, not filepath.Join, because Join
// cleans the ".." away — exactly the transformation the check must not depend on.
func TestIsPathInWorkspaceDotDotAfterSymlink(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(ws, "escape")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	raw := map[string]string{
		"read existing via symlink+..":      ws + "/escape/../" + filepath.Base(outside) + "/secret",
		"write non-existent via symlink+..": ws + "/escape/../newfile",
		"plain parent traversal":            ws + "/../etc/passwd",
	}
	for name, target := range raw {
		t.Run(name, func(t *testing.T) {
			if policy.IsPathInWorkspace(target, ws) {
				t.Errorf("IsPathInWorkspace(%q, %q) = true, want false (traversal must be rejected)", target, ws)
			}
		})
	}
}

// TestIsPathInWorkspaceSymlinkNoDotDot confirms the legitimate-resolution path:
// a symlink that escapes the workspace is caught by symlink resolution even
// without any ".." segment, for both existing and non-existent tails.
func TestIsPathInWorkspaceSymlinkNoDotDot(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(ws, "escape")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	for name, target := range map[string]string{
		"existing outside file":     ws + "/escape/secret",
		"non-existent outside tail": ws + "/escape/newfile",
	} {
		t.Run(name, func(t *testing.T) {
			if policy.IsPathInWorkspace(target, ws) {
				t.Errorf("IsPathInWorkspace(%q, %q) = true, want false (escaping symlink)", target, ws)
			}
		})
	}
}

func TestIsPathInWorkspaceFailClosed(t *testing.T) {
	// A workspace that does not exist cannot be canonicalized → fail closed.
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if policy.IsPathInWorkspace(filepath.Join(missing, "f"), missing) {
		t.Error("non-existent workspace did not fail closed")
	}
}
