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

package hook

import (
	"errors"
	"testing"
)

// unknownHook satisfies the sealed Hook interface but is not one of the
// concrete hook types, exercising Register's default (unknown-type) branch.
// It can only be declared inside the hook package because isHook is unexported
// — which is the point of sealing the interface.
type unknownHook struct{}

func (unknownHook) isHook() {}

func TestRegisterUnknownHook(t *testing.T) {
	r := NewRunner()
	err := r.Register(unknownHook{})
	var unk *ErrUnknownHook
	if !errors.As(err, &unk) {
		t.Fatalf("Register(unknown) error = %v, want *ErrUnknownHook", err)
	}
}
