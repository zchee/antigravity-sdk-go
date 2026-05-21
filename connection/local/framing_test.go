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
	"bytes"
	"encoding/binary"
	"testing"

	"google.golang.org/protobuf/proto"

	pb "github.com/zchee/antigravity-sdk-go/internal/localharnesspb"
)

func TestFrameRoundTrip(t *testing.T) {
	in := pb.InputConfig_builder{StorageDirectory: proto.String("/tmp/save")}.Build()
	var buf bytes.Buffer
	if err := writeFrame(&buf, in); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	// The first 4 bytes are the little-endian length of the body.
	wantLen, _ := proto.Marshal(in)
	if got := binary.LittleEndian.Uint32(buf.Bytes()[:4]); int(got) != len(wantLen) {
		t.Errorf("framed length = %d, want %d", got, len(wantLen))
	}

	out := &pb.InputConfig{}
	if err := readFrame(&buf, out); err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if out.GetStorageDirectory() != "/tmp/save" {
		t.Errorf("round-tripped storage_directory = %q, want /tmp/save", out.GetStorageDirectory())
	}
}

func TestReadFrameTruncated(t *testing.T) {
	// A buffer with a length header promising more bytes than present must error
	// rather than hang or panic.
	var buf bytes.Buffer
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], 100)
	buf.Write(hdr[:])
	buf.Write([]byte{1, 2, 3}) // only 3 of 100 bytes
	if err := readFrame(&buf, &pb.InputConfig{}); err == nil {
		t.Error("readFrame on truncated body = nil error, want failure")
	}
}

func TestResolveBinaryPathEnv(t *testing.T) {
	t.Setenv(HarnessPathEnv, "/custom/localharness")
	got, err := resolveBinaryPath("")
	if err != nil {
		t.Fatalf("resolveBinaryPath: %v", err)
	}
	if got != "/custom/localharness" {
		t.Errorf("resolveBinaryPath = %q, want /custom/localharness", got)
	}
}

func TestResolveBinaryPathExplicitBeatsEnv(t *testing.T) {
	t.Setenv(HarnessPathEnv, "/from/env")
	got, err := resolveBinaryPath("/from/field")
	if err != nil {
		t.Fatalf("resolveBinaryPath: %v", err)
	}
	if got != "/from/field" {
		t.Errorf("resolveBinaryPath = %q, want /from/field", got)
	}
}

func TestResolveBinaryPathNotFound(t *testing.T) {
	t.Setenv(HarnessPathEnv, "")
	t.Setenv("PATH", t.TempDir())
	if _, err := resolveBinaryPath(""); err == nil {
		t.Error("resolveBinaryPath with no binary = nil error, want ErrBinaryNotFound")
	}
}
