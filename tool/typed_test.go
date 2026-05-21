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

package tool_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zchee/antigravity-sdk-go/tool"
)

type addArgs struct {
	X int `json:"x" jsonschema:"first addend"`
	Y int `json:"y" jsonschema:"second addend"`
}

func TestTypedHappyPath(t *testing.T) {
	got, err := tool.Typed("add", "Adds two integers.",
		func(_ context.Context, a addArgs) (any, error) { return a.X + a.Y, nil })
	if err != nil {
		t.Fatalf("Typed: %v", err)
	}
	if got.Name != "add" {
		t.Errorf("Name = %q, want add", got.Name)
	}
	if got.Description != "Adds two integers." {
		t.Errorf("Description = %q", got.Description)
	}
	if got.InputSchema["type"] != "object" {
		t.Errorf("schema type = %v, want object", got.InputSchema["type"])
	}
	props, ok := got.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %v (%T), want map", got.InputSchema["properties"], got.InputSchema["properties"])
	}
	if _, ok := props["x"]; !ok {
		t.Errorf("schema missing property x; got keys %v", props)
	}
	if _, ok := props["y"]; !ok {
		t.Errorf("schema missing property y; got keys %v", props)
	}

	result, err := got.Fn(t.Context(), map[string]any{"x": 2, "y": 3})
	if err != nil {
		t.Fatalf("Fn: %v", err)
	}
	if result != 5 {
		t.Errorf("result = %v, want 5", result)
	}
}

func TestTypedEmptyArgsZeroValue(t *testing.T) {
	type noArgs struct{}
	called := false
	tw, err := tool.Typed("noop", "",
		func(_ context.Context, _ noArgs) (any, error) {
			called = true
			return "ok", nil
		})
	if err != nil {
		t.Fatalf("Typed: %v", err)
	}
	out, err := tw.Fn(t.Context(), nil)
	if err != nil {
		t.Fatalf("Fn(nil): %v", err)
	}
	if !called {
		t.Error("fn not invoked")
	}
	if out != "ok" {
		t.Errorf("out = %v", out)
	}
}

func TestTypedDecodeFailure(t *testing.T) {
	tw, err := tool.Typed("add", "",
		func(_ context.Context, _ addArgs) (any, error) { return nil, nil })
	if err != nil {
		t.Fatalf("Typed: %v", err)
	}
	// "x" is supposed to be an int; passing a string forces a decode error.
	_, err = tw.Fn(t.Context(), map[string]any{"x": "not-a-number"})
	if err == nil {
		t.Fatal("Fn with bad arg = nil error, want decode failure")
	}
	var aie *tool.ErrToolArgsInvalid
	if !errors.As(err, &aie) {
		t.Fatalf("err type = %T (%v), want *tool.ErrToolArgsInvalid", err, err)
	}
	if aie.Name != "add" {
		t.Errorf("aie.Name = %q, want add", aie.Name)
	}
	if !errors.Is(err, tool.ErrTypedArgs) {
		t.Errorf("errors.Is(err, ErrTypedArgs) = false, want true")
	}
}

func TestTypedFnErrorPropagates(t *testing.T) {
	sentinel := errors.New("boom")
	tw, err := tool.Typed("err", "",
		func(_ context.Context, _ addArgs) (any, error) { return nil, sentinel })
	if err != nil {
		t.Fatalf("Typed: %v", err)
	}
	_, err = tw.Fn(t.Context(), map[string]any{"x": 1, "y": 1})
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want wrapping sentinel", err)
	}
}

func TestSchemaForCycleSurfacesError(t *testing.T) {
	type cyclic struct {
		Next *cyclic `json:"next,omitempty"`
	}
	_, err := tool.SchemaFor[cyclic]()
	if err == nil {
		t.Error("SchemaFor on recursive type = nil error, want failure")
	}
}

func TestMustTypedPanicsOnBadType(t *testing.T) {
	type cyclic struct {
		Next *cyclic `json:"next,omitempty"`
	}
	defer func() {
		if recover() == nil {
			t.Error("MustTyped on recursive type did not panic")
		}
	}()
	tool.MustTyped[cyclic]("bad", "", func(context.Context, cyclic) (any, error) { return nil, nil })
}

func TestTypedRegistersViaAddTool(t *testing.T) {
	r := tool.NewRunner()
	tw, err := tool.Typed("add", "",
		func(_ context.Context, a addArgs) (any, error) { return a.X + a.Y, nil })
	if err != nil {
		t.Fatalf("Typed: %v", err)
	}
	if err := r.AddTool(tw); err != nil {
		t.Fatalf("AddTool: %v", err)
	}
	if _, ok := r.Schema("add"); !ok {
		t.Error("schema not registered for add")
	}
	out, err := r.Execute(t.Context(), "add", map[string]any{"x": 10, "y": 32})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != 42 {
		t.Errorf("Execute add = %v, want 42", out)
	}
}
