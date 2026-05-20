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
	"fmt"

	gojson "github.com/go-json-experiment/json"
	"google.golang.org/protobuf/proto"

	"github.com/zchee/antigravity-sdk-go/agtypes"
	pb "github.com/zchee/antigravity-sdk-go/internal/localharnesspb"
)

// toolProto builds a localharness Tool proto from a registered host tool. The
// upstream callable_to_tool_proto used google.genai FunctionDeclaration to
// infer a schema from a bare Python callable; the Go port has no bare callables
// — every tool is a tool.ToolWithSchema carrying an explicit JSON schema — so
// the schema is used directly. name and description come from the tool's
// registration (Go funcs carry neither).
func toolProto(name, description string, inputSchema map[string]any) (*pb.Tool, error) {
	schema := inputSchema
	if schema == nil {
		schema = map[string]any{"type": "OBJECT"}
	}
	b, err := gojson.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("local: marshal tool %q schema: %w", name, err)
	}
	return pb.Tool_builder{
		Name:                 proto.String(name),
		Description:          proto.String(description),
		ParametersJsonSchema: proto.String(string(b)),
	}.Build(), nil
}

// toProtoInputContent converts one prompt content primitive (a string or an
// agtypes.Media) into a UserInput_Part. It returns an error for an unsupported
// dynamic type.
func toProtoInputContent(content agtypes.ContentPrimitive) (*pb.UserInput_Part, error) {
	switch c := content.(type) {
	case string:
		return pb.UserInput_Part_builder{Text: proto.String(c)}.Build(), nil
	case agtypes.Media:
		media := pb.UserInput_Media_builder{
			MimeType:    proto.String(c.MIME()),
			Description: proto.String(c.Desc()),
			Data:        c.Bytes(),
		}.Build()
		return pb.UserInput_Part_builder{Media: media}.Build(), nil
	default:
		return nil, fmt.Errorf("local: unsupported prompt content type %T", content)
	}
}
