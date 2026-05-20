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

// Package local implements the Connection to the bundled Go localharness
// binary.
//
// The connection spawns the binary, performs a length-prefixed protobuf
// handshake over the process's stdin/stdout to learn a websocket port, then
// exchanges protojson-encoded localharness messages over that websocket. See
// Strategy for the lifecycle and Connection for the runtime behavior.
package local

import (
	"fmt"
	"strings"
)

// ToolOutput is the structured result of a builtin tool execution, extracted
// from the per-action fields of a StepUpdate. Implementations are closed to
// this package via the unexported marker method; each has a String method
// giving the human-readable form the model sees when no richer encoding
// applies.
type ToolOutput interface {
	isToolOutput()
	String() string
}

// RunCommandResult is the structured result of a run_command execution.
type RunCommandResult struct {
	Output string
}

func (RunCommandResult) isToolOutput()    {}
func (r RunCommandResult) String() string { return r.Output }

// ListDirectoryEntry is a single entry in a directory listing.
type ListDirectoryEntry struct {
	Name        string
	IsDirectory bool
	FileSize    int64
}

// ListDirectoryResult is the structured result of a list_directory execution.
type ListDirectoryResult struct {
	Entries []ListDirectoryEntry
}

func (ListDirectoryResult) isToolOutput() {}

// String renders each entry one per line: "name/ (dir)" for directories,
// "name (N bytes)" for files.
func (r ListDirectoryResult) String() string {
	var b strings.Builder
	for i, e := range r.Entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		if e.IsDirectory {
			fmt.Fprintf(&b, "%s/ (dir)", e.Name)
		} else {
			fmt.Fprintf(&b, "%s (%d bytes)", e.Name, e.FileSize)
		}
	}
	return b.String()
}

// SearchDirectoryResult is the structured result of a search_directory
// execution.
type SearchDirectoryResult struct {
	NumResults int
}

func (SearchDirectoryResult) isToolOutput()    {}
func (r SearchDirectoryResult) String() string { return fmt.Sprintf("%d results", r.NumResults) }

// FindFileResult is the structured result of a find_file execution.
type FindFileResult struct {
	Output string
}

func (FindFileResult) isToolOutput()    {}
func (r FindFileResult) String() string { return r.Output }

// EditFileResult is the structured result of an edit_file execution.
type EditFileResult struct {
	Summary string
}

func (EditFileResult) isToolOutput()    {}
func (r EditFileResult) String() string { return r.Summary }

// GenerateImageResult is the structured result of a generate_image execution.
type GenerateImageResult struct {
	ImageName string
}

func (GenerateImageResult) isToolOutput()    {}
func (r GenerateImageResult) String() string { return r.ImageName }

// TextResult is the generic fallback for tools without a structured output
// (e.g. view_file).
type TextResult struct {
	Text string
}

func (TextResult) isToolOutput()    {}
func (r TextResult) String() string { return r.Text }
