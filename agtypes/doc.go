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

// Package agtypes defines the canonical SDK boundary types for the Google
// Antigravity Go SDK.
//
// These are the public types used across every SDK interface. They are the
// dependency root of the module: agtypes imports nothing else from this
// repository. The Go port of the upstream Python SDK keeps these in their own
// package (rather than the root antigravity package) so that the connection
// layer can reference them without creating an import cycle with the top-level
// Agent/Conversation types, which themselves depend on the connection layer.
//
// The types correspond to the Pydantic V2 models in the upstream
// google/antigravity/types.py. Where the upstream relies on Pydantic
// validators, the Go port exposes a Validate method or a constructor that
// returns an error; see CapabilitiesConfig.Validate and the media constructors.
//
// JSON struct tags use snake_case to match the upstream serialization names.
// These types do not themselves cross the wire to the localharness binary —
// that boundary is protobuf (see internal/localharnesspb) — so the JSON tags
// exist for user-facing config (de)serialization only.
package agtypes
