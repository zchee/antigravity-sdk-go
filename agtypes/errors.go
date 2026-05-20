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

package agtypes

import "fmt"

// ConnectionError is the base error for connection failures in the SDK. It is
// returned when a connection to an agent backend cannot be established or
// encounters a fatal protocol-level error.
//
// It mirrors the upstream AntigravityConnectionError. Wrap an underlying cause
// with Err to support errors.Is/errors.As unwrapping.
type ConnectionError struct {
	// Message is a human-readable error description.
	Message string
	// Err is an optional underlying cause.
	Err error
}

// Error implements the error interface.
func (e *ConnectionError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("antigravity: connection error: %s: %v", e.Message, e.Err)
	}
	return fmt.Sprintf("antigravity: connection error: %s", e.Message)
}

// Unwrap returns the underlying cause, if any.
func (e *ConnectionError) Unwrap() error { return e.Err }

// ValidationError wraps a validation failure at the SDK boundary.
//
// It mirrors the upstream AntigravityValidationError. The upstream
// from_pydantic constructor has no Go analog and is intentionally omitted;
// wrap Go validation errors directly via Err.
type ValidationError struct {
	// Message is a human-readable error description.
	Message string
	// Err is an optional underlying cause.
	Err error
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("antigravity: validation error: %s: %v", e.Message, e.Err)
	}
	return fmt.Sprintf("antigravity: validation error: %s", e.Message)
}

// Unwrap returns the underlying cause, if any.
func (e *ValidationError) Unwrap() error { return e.Err }
