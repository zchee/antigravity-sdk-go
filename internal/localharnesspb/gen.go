// Package localharnesspb contains the Go bindings for the localharness wire
// protocol spoken by the bundled Go `localharness` binary that the upstream
// antigravity-sdk-python SDK spawns.
//
// # Provenance
//
// The upstream Python SDK ships only the generated descriptor
// (google/antigravity/connections/local/localharness_pb2.py); there is no
// `.proto` source in the repository. The serialized FileDescriptorProto
// embedded in that file is therefore the authoritative source for these
// bindings.
//
// localharness.fds is a FileDescriptorSet built by wrapping that embedded
// FileDescriptorProto (decoded from the AddSerializedFile(...) byte literal in
// localharness_pb2.py). It is the input to protoc and is checked in as the
// source of truth — do NOT hand-edit localharness.pb.go.
//
// Source: github.com/google-antigravity/antigravity-sdk-python @ main
//
//	repo commit:                  287894d3b5689b99fcea97900d05cfa7fe93fcbf
//	localharness_pb2.py blob sha: b51c2f3df29cfd71688b1bd54058c23dd61dfc19
//
// # Regenerating
//
// To refresh against upstream, rebuild localharness.fds from the current
// localharness_pb2.py, then regenerate:
//
//	# 1. Fetch the embedded descriptor and rebuild the FileDescriptorSet.
//	#    The descriptor is the byte argument to AddSerializedFile(...) in
//	#    localharness_pb2.py; wrap it as a FileDescriptorSet (field 1, the
//	#    `file` repeated FileDescriptorProto) and write localharness.fds.
//	#
//	# 2. Regenerate the Go bindings from the checked-in descriptor set:
//	protoc \
//	  --descriptor_set_in=localharness.fds \
//	  --go_out=. \
//	  --go_opt=Mlocalharness.proto=github.com/zchee/antigravity-sdk-go/internal/localharnesspb \
//	  --go_opt=module=github.com/zchee/antigravity-sdk-go/internal/localharnesspb \
//	  localharness.proto
//
// The bindings use the protobuf opaque API (protogen:"opaque.v1"): message
// fields are unexported and accessed via Get*/Set* methods and the
// <Message>_builder{...}.Build() pattern. Construct messages with builders,
// never struct literals.
//
//go:generate protoc --descriptor_set_in=localharness.fds --go_out=. --go_opt=Mlocalharness.proto=github.com/zchee/antigravity-sdk-go/internal/localharnesspb --go_opt=module=github.com/zchee/antigravity-sdk-go/internal/localharnesspb localharness.proto
package localharnesspb
