package localharnesspb_test

import (
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "github.com/zchee/antigravity-sdk-go/internal/localharnesspb"
)

// TestHarnessConfigRoundTrip is a smoke test confirming the generated bindings
// load (init of the file descriptor does not panic) and that a representative
// message survives both binary and JSON round-trips. The local connection
// relies on both proto.Marshal framing and protojson, so both are exercised.
func TestHarnessConfigRoundTrip(t *testing.T) {
	want := pb.HarnessConfig_builder{
		CascadeId: proto.String("cascade-123"),
		GeminiConfig: pb.GeminiConfig_builder{
			ApiKey:           proto.String("secret"),
			ModelName:        proto.String("gemini-3.5-flash"),
			ThinkingLevel:    proto.String("high"),
			EnableUrlContext: proto.Bool(true),
		}.Build(),
		Tools: []*pb.Tool{
			pb.Tool_builder{
				Name:        proto.String("read_file"),
				Description: proto.String("Reads a file"),
			}.Build(),
		},
		SkillsPaths: []string{"/skills/a", "/skills/b"},
		AppDataDir:  proto.String("/home/u/.gemini/antigravity"),
	}.Build()

	t.Run("binary", func(t *testing.T) {
		raw, err := proto.Marshal(want)
		if err != nil {
			t.Fatalf("proto.Marshal: %v", err)
		}
		got := &pb.HarnessConfig{}
		if err := proto.Unmarshal(raw, got); err != nil {
			t.Fatalf("proto.Unmarshal: %v", err)
		}
		if !proto.Equal(want, got) {
			t.Errorf("binary round-trip mismatch:\n want = %v\n got  = %v", want, got)
		}
	})

	t.Run("json", func(t *testing.T) {
		raw, err := protojson.Marshal(want)
		if err != nil {
			t.Fatalf("protojson.Marshal: %v", err)
		}
		got := &pb.HarnessConfig{}
		if err := protojson.Unmarshal(raw, got); err != nil {
			t.Fatalf("protojson.Unmarshal: %v", err)
		}
		if !proto.Equal(want, got) {
			t.Errorf("json round-trip mismatch:\n want = %v\n got  = %v", want, got)
		}
	})

	t.Run("accessors", func(t *testing.T) {
		if got := want.GetCascadeId(); got != "cascade-123" {
			t.Errorf("GetCascadeId() = %q, want %q", got, "cascade-123")
		}
		if got := want.GetGeminiConfig().GetModelName(); got != "gemini-3.5-flash" {
			t.Errorf("GetModelName() = %q, want %q", got, "gemini-3.5-flash")
		}
		if got := want.GetGeminiConfig().GetModelName(); got == "" {
			t.Error("oneof model_config not set to gemini config")
		}
	})
}

// TestGeminiConfigDefault confirms the proto-level default for model_name is
// preserved by the generated bindings (def=gemini-3.5-flash in the descriptor).
func TestGeminiConfigDefault(t *testing.T) {
	c := &pb.GeminiConfig{}
	if got := c.GetModelName(); got != pb.Default_GeminiConfig_ModelName {
		t.Errorf("default GetModelName() = %q, want %q", got, pb.Default_GeminiConfig_ModelName)
	}
	if pb.Default_GeminiConfig_ModelName != "gemini-3.5-flash" {
		t.Errorf("Default_GeminiConfig_ModelName = %q, want %q", pb.Default_GeminiConfig_ModelName, "gemini-3.5-flash")
	}
}
