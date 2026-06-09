package runtime

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestPermissionSnapshotIgnorePermissions(t *testing.T) {
	rt := &Runtime{IgnorePermissions: true}

	snap := rt.PermissionSnapshot()

	if !snap.Known {
		t.Fatal("expected ignore-permissions snapshot to be known")
	}

	if len(snap.Raw) != 1 || snap.Raw[0] != "*" {
		t.Fatalf("unexpected ignore-permissions scopes: %v", snap.Raw)
	}
}

func TestLoadDescriptorFilesRejectsBadInput(t *testing.T) {
	tests := []struct {
		name          string
		input         []byte
		wantErrSubstr string
	}{
		{name: "empty set", input: nil, wantErrSubstr: "empty"},
		{name: "invalid data", input: []byte("not-a-descriptor-set"), wantErrSubstr: "unmarshal descriptor set"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadDescriptorFiles(tt.input)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErrSubstr) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoadDescriptorFilesLoadsReturnedRegistry(t *testing.T) {
	path := t.Name() + ".proto"

	raw, err := proto.Marshal(&descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{{
			Name:    proto.String(path),
			Package: proto.String("test.v1"),
			Syntax:  proto.String("proto3"),
		}},
	})
	if err != nil {
		t.Fatalf("marshal descriptor set: %v", err)
	}

	files, err := LoadDescriptorFiles(raw)
	if err != nil {
		t.Fatalf("LoadDescriptorFiles: %v", err)
	}

	if _, err := files.FindFileByPath(path); err != nil {
		t.Fatalf("expected descriptor in returned registry: %v", err)
	}
}
