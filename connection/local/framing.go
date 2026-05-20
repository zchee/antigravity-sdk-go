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
	"encoding/binary"
	"fmt"
	"io"

	"google.golang.org/protobuf/proto"
)

// writeFrame writes a length-prefixed protobuf message to w. The prefix is a
// 4-byte little-endian uint32 length (matching the upstream struct.pack("<I"))
// followed by the serialized message bytes. It is used only for the stdin
// handshake; websocket messages are protojson, not framed.
func writeFrame(w io.Writer, m proto.Message) error {
	data, err := proto.Marshal(m)
	if err != nil {
		return fmt.Errorf("local: marshal frame: %w", err)
	}
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(data)))
	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("local: write frame length: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("local: write frame body: %w", err)
	}
	return nil
}

// readFrame reads a length-prefixed protobuf message from r into m, using the
// same 4-byte little-endian framing as writeFrame. It is used to read the
// OutputConfig the harness emits on stdout during the handshake.
func readFrame(r io.Reader, m proto.Message) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return fmt.Errorf("local: read frame length: %w", err)
	}
	n := binary.LittleEndian.Uint32(hdr[:])
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return fmt.Errorf("local: read frame body: %w", err)
	}
	if err := proto.Unmarshal(body, m); err != nil {
		return fmt.Errorf("local: unmarshal frame: %w", err)
	}
	return nil
}
