package proto

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/olcf/s3m-cli/internal/headermap"
)

func TestCollectMethodsSkipsStreaming(t *testing.T) {
	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("collect.proto"),
		Package: proto.String("compute.v1"),
		Syntax:  proto.String("proto3"),
		Dependency: []string{
			emptypb.File_google_protobuf_empty_proto.Path(),
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("Jobs"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{
					Name:       proto.String("ListJobs"),
					InputType:  proto.String(".google.protobuf.Empty"),
					OutputType: proto.String(".google.protobuf.Empty"),
				},
				{
					Name:            proto.String("WatchJobs"),
					InputType:       proto.String(".google.protobuf.Empty"),
					OutputType:      proto.String(".google.protobuf.Empty"),
					ServerStreaming: proto.Bool(true),
				},
				{
					Name:            proto.String("StreamJobs"),
					InputType:       proto.String(".google.protobuf.Empty"),
					OutputType:      proto.String(".google.protobuf.Empty"),
					ClientStreaming: proto.Bool(true),
					ServerStreaming: proto.Bool(true),
				},
			},
		}},
	}

	methods := CollectMethods(buildTestFiles(
		t,
		fdp,
		protodesc.ToFileDescriptorProto(emptypb.File_google_protobuf_empty_proto),
	), headermap.DefaultEnclave, headermap.DefaultResolver())
	if len(methods) != 1 {
		t.Fatalf("expected only unary methods, got %d", len(methods))
	}

	m := methods[0]
	if m.API != "compute" || m.Version != "v1" {
		t.Fatalf("unexpected API/version: %q %q", m.API, m.Version)
	}
	if m.ToolName != "Jobs_v1_ListJobs" {
		t.Fatalf("unexpected ToolName: %s", m.ToolName)
	}
	if m.Path != "/compute/v1/listjobs" {
		t.Fatalf("unexpected Path: %s", m.Path)
	}
	if m.Desc == "" || m.Method == nil || m.Service == nil {
		t.Fatalf("method info incomplete: %+v", m)
	}
}
