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
	"errors"
	"os"
	"os/exec"
)

// HarnessPathEnv is the environment variable that, when set, gives the explicit
// path to the localharness binary.
const HarnessPathEnv = "ANTIGRAVITY_HARNESS_PATH"

// ErrBinaryNotFound reports that the localharness binary could not be located.
var ErrBinaryNotFound = errors.New("local: could not find the localharness binary; " +
	"set " + HarnessPathEnv + " or ensure 'localharness' is on PATH")

// resolveBinaryPath locates the localharness binary, mirroring the upstream
// resolution order minus the Python-package-resource branch (which has no Go
// analog):
//
//  1. the ANTIGRAVITY_HARNESS_PATH environment variable, if set;
//  2. a "localharness" executable on PATH.
//
// It returns ErrBinaryNotFound if neither yields a usable path.
func resolveBinaryPath() (string, error) {
	if env := os.Getenv(HarnessPathEnv); env != "" {
		return env, nil
	}
	if path, err := exec.LookPath("localharness"); err == nil {
		return path, nil
	}
	return "", ErrBinaryNotFound
}
