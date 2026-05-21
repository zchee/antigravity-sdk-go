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
	"context"
	"errors"
	"os"
	"os/exec"
)

// HarnessPathEnv is the environment variable that, when set, gives the explicit
// path to the localharness binary.
const HarnessPathEnv = "ANTIGRAVITY_HARNESS_PATH"

// ErrBinaryNotFound reports that the localharness binary could not be located.
var ErrBinaryNotFound = errors.New("local: could not find the localharness binary; " +
	"set " + HarnessPathEnv + ", AgentConfig.HarnessPath, AgentConfig.HarnessProvider, " +
	"or ensure 'localharness' is on PATH")

// HarnessProvider yields the path to a localharness binary, optionally backed
// by an extracted asset. It is the extension point downstream uses to ship the
// harness inside a Go binary via //go:embed: the provider writes the embedded
// bytes to a tempfile, returns the path, and returns a cleanup that removes
// the tempfile when the Strategy closes.
//
// cleanup may be nil if there is nothing to release. It is called exactly once,
// during Strategy.Close, regardless of whether Start succeeded after the
// provider returned.
type HarnessProvider func(ctx context.Context) (path string, cleanup func(), err error)

// resolveBinaryPath locates the localharness binary by checking, in order:
//
//  1. an explicit path supplied by the caller (AgentConfig.HarnessPath);
//  2. the ANTIGRAVITY_HARNESS_PATH environment variable;
//  3. a "localharness" executable on PATH.
//
// HarnessProvider is handled separately by Strategy.Start (because it produces
// a cleanup callback and needs a context). It returns ErrBinaryNotFound if
// none of the static sources yields a usable path.
func resolveBinaryPath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if env := os.Getenv(HarnessPathEnv); env != "" {
		return env, nil
	}
	if path, err := exec.LookPath("localharness"); err == nil {
		return path, nil
	}
	return "", ErrBinaryNotFound
}
