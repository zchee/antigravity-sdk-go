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

package tool

import (
	"context"
	"errors"
	"fmt"

	gojson "github.com/go-json-experiment/json"
	"github.com/google/jsonschema-go/jsonschema"
)

// SchemaFor returns the JSON Schema for T as a map[string]any, ready to assign
// to ToolWithSchema.InputSchema. The schema is inferred by jsonschema.For,
// which translates exported struct fields (using their JSON names), tags
// `omitzero`/`omitempty` fields as optional, and rejects recursive types,
// function/channel/complex fields, and non-string map keys.
//
// A `jsonschema:"…"` struct field tag becomes that field's description in the
// emitted schema. See jsonschema-go's package docs for the full tag grammar.
//
// SchemaFor returns the error from jsonschema.For unchanged; it never panics
// on incompatible types.
func SchemaFor[T any]() (map[string]any, error) {
	s, err := jsonschema.For[T](nil)
	if err != nil {
		return nil, fmt.Errorf("tool: derive schema for %T: %w", *new(T), err)
	}
	raw, err := gojson.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("tool: marshal derived schema for %T: %w", *new(T), err)
	}
	var out map[string]any
	if err := gojson.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("tool: round-trip derived schema for %T: %w", *new(T), err)
	}
	return out, nil
}

// ErrToolArgsInvalid reports that a tool's incoming arguments did not decode
// into the typed Args struct expected by a Typed tool. Unwrap to inspect the
// underlying json error.
type ErrToolArgsInvalid struct {
	Name string
	Err  error
}

func (e *ErrToolArgsInvalid) Error() string {
	return fmt.Sprintf("tool: %q arguments invalid: %v", e.Name, e.Err)
}

func (e *ErrToolArgsInvalid) Unwrap() error { return e.Err }

// Typed wraps a typed Go function as a ToolWithSchema, deriving the input
// schema from Args via SchemaFor. fn is invoked after the runner's
// map[string]any arguments are decoded into an Args value. A decode failure
// surfaces as *ErrToolArgsInvalid; fn's own error is returned unchanged.
//
// Typed exists so callers can register a typed function directly instead of
// writing the schema by hand and unpacking map[string]any in the body. It
// returns an error only when SchemaFor does — i.e. when Args itself is
// incompatible with JSON Schema (recursive, contains channels, etc.).
//
// Example:
//
//	type AddArgs struct {
//	    X int `json:"x" jsonschema:"first addend"`
//	    Y int `json:"y" jsonschema:"second addend"`
//	}
//	add, err := tool.Typed("add", "Adds two integers.",
//	    func(_ context.Context, a AddArgs) (int, error) { return a.X + a.Y, nil })
//	if err != nil { return err }
//	runner.AddTool(add)
func Typed[Args any](name, description string, fn func(ctx context.Context, args Args) (any, error)) (ToolWithSchema, error) {
	schema, err := SchemaFor[Args]()
	if err != nil {
		return ToolWithSchema{}, err
	}
	wrapped := func(ctx context.Context, raw map[string]any) (any, error) {
		args, decErr := decodeArgs[Args](raw)
		if decErr != nil {
			return nil, &ErrToolArgsInvalid{Name: name, Err: decErr}
		}
		return fn(ctx, args)
	}
	return ToolWithSchema{
		Name:        name,
		Description: description,
		Fn:          wrapped,
		InputSchema: schema,
	}, nil
}

// MustTyped is the panicking variant of Typed for tools whose Args type is
// known at compile time to be schema-compatible. Useful in package-level var
// declarations where returning an error is awkward.
func MustTyped[Args any](name, description string, fn func(ctx context.Context, args Args) (any, error)) ToolWithSchema {
	t, err := Typed(name, description, fn)
	if err != nil {
		panic(err)
	}
	return t
}

// decodeArgs converts the runner's untyped argument map into a typed Args
// value by round-tripping through JSON. nil/empty maps decode to the zero
// value of Args, matching what callers naturally pass for no-argument tools.
func decodeArgs[Args any](raw map[string]any) (Args, error) {
	var args Args
	if len(raw) == 0 {
		return args, nil
	}
	buf, err := gojson.Marshal(raw)
	if err != nil {
		return args, fmt.Errorf("re-marshal: %w", err)
	}
	if err := gojson.Unmarshal(buf, &args); err != nil {
		return args, err
	}
	return args, nil
}

// errTyped is a sentinel kept for downstream errors.Is checks against the
// generic "args invalid" condition, irrespective of the tool name.
var errTyped = errors.New("tool: typed-args decode failed")

// errors.Is(err, errTyped) returns true when err is an *ErrToolArgsInvalid.
// The method satisfies the unwrap-by-target form used by errors.Is.
func (e *ErrToolArgsInvalid) Is(target error) bool {
	return target == errTyped
}

// ErrTypedArgs is the comparable sentinel returned by errors.Is when matching
// any *ErrToolArgsInvalid. Use it when the caller does not care which tool
// produced the decode failure.
var ErrTypedArgs = errTyped
