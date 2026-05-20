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

package local

import (
	"bufio"
	"io"
	"strings"
	"sync"

	gojson "github.com/go-json-experiment/json"
	"google.golang.org/protobuf/proto"
)

// proto scalar pointer helpers, aliased for brevity in builder literals.
var (
	protoString = proto.String
	protoBool   = proto.Bool
	protoUint32 = proto.Uint32
)

// gojsonUnmarshal unmarshals a JSON string into v using the experimental json
// package (the module's standard JSON codec).
func gojsonUnmarshal(s string, v any) error {
	return gojson.Unmarshal([]byte(s), v)
}

// stderrBuffer captures the most recent lines of the harness's stderr in a
// bounded ring, so an unexpected exit can surface diagnostic context. It is
// safe for concurrent use; a background goroutine drains the pipe so the
// harness never blocks on a full stderr buffer.
type stderrBuffer struct {
	mu    sync.Mutex
	lines []string
	max   int
}

// newStderrBuffer returns a buffer retaining the last max lines and starts a
// goroutine draining r into it.
func newStderrBuffer(r io.Reader, max int) *stderrBuffer {
	b := &stderrBuffer{max: max}
	go b.drain(r)
	return b
}

func (b *stderrBuffer) drain(r io.Reader) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		b.mu.Lock()
		b.lines = append(b.lines, sc.Text())
		if len(b.lines) > b.max {
			b.lines = b.lines[len(b.lines)-b.max:]
		}
		b.mu.Unlock()
	}
}

// tail returns the retained stderr lines joined by newlines.
func (b *stderrBuffer) tail() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Join(b.lines, "\n")
}
