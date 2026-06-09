package proto

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestParsePackageInfoExtractsAPIAndVersion(t *testing.T) {
	api, version := ParsePackageInfo("foo.bar.v2")
	if api != "bar" || version != "v2" {
		t.Fatalf("unexpected parse result: api=%q version=%q", api, version)
	}
}

func TestParsePackageInfoDefaultsWhenNoVersion(t *testing.T) {
	api, version := ParsePackageInfo("foo.bar.baz")
	if api != "default" || version != "" {
		t.Fatalf("expected defaults, got api=%q version=%q", api, version)
	}
}

func TestToolNameForMethodTruncatesAndSanitizes(t *testing.T) {
	longService := "VeryLongServiceNameThatExceedsTheMaximumAllowedLengthForToolNamesToEnsureTruncation"
	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("naming.proto"),
		Package: proto.String("demo.v1"),
		Syntax:  proto.String("proto3"),
		Dependency: []string{
			emptypb.File_google_protobuf_empty_proto.Path(),
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String(longService),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       proto.String("Call"),
				InputType:  proto.String(".google.protobuf.Empty"),
				OutputType: proto.String(".google.protobuf.Empty"),
			}},
		}},
	}

	fd := buildTestFileDescriptor(
		t,
		fdp,
		protodesc.ToFileDescriptorProto(emptypb.File_google_protobuf_empty_proto),
	)
	sd := fd.Services().ByName(protoreflect.Name(longService))
	md := sd.Methods().ByName("Call")

	tool := ToolNameForMethod(fd, sd, md)
	if len(tool) > ToolNameMaxLen {
		t.Fatalf("expected tool name to be truncated to %d, got %d", ToolNameMaxLen, len(tool))
	}
	if wantSuffix := "v1_Call"; tool[len(tool)-len(wantSuffix):] != wantSuffix {
		t.Fatalf("unexpected tool name suffix: %q", tool)
	}
}

func TestBuildRESTPathLowercasesComponents(t *testing.T) {
	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("path.proto"),
		Package: proto.String("Compute.v1"),
		Syntax:  proto.String("proto3"),
		Dependency: []string{
			emptypb.File_google_protobuf_empty_proto.Path(),
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("Jobs"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       proto.String("ListJobs"),
				InputType:  proto.String(".google.protobuf.Empty"),
				OutputType: proto.String(".google.protobuf.Empty"),
			}},
		}},
	}

	fd := buildTestFileDescriptor(
		t,
		fdp,
		protodesc.ToFileDescriptorProto(emptypb.File_google_protobuf_empty_proto),
	)
	md := fd.Services().ByName("Jobs").Methods().ByName("ListJobs")

	path := BuildRESTPath("Compute", "V1", md)
	if path != "/compute/V1/listjobs" {
		t.Fatalf("unexpected path: %s", path)
	}
}

func buildTestFileDescriptor(
	t *testing.T, fdp *descriptorpb.FileDescriptorProto, deps ...*descriptorpb.FileDescriptorProto,
) protoreflect.FileDescriptor {
	t.Helper()

	all := append([]*descriptorpb.FileDescriptorProto{fdp}, deps...)

	files := buildTestFiles(t, all...)

	fd, err := files.FindFileByPath(fdp.GetName())
	if err != nil {
		t.Fatalf("FindFileByPath: %v", err)
	}

	return fd
}

func buildTestFiles(t *testing.T, files ...*descriptorpb.FileDescriptorProto) *protoregistry.Files {
	t.Helper()

	fset, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: files})
	if err != nil {
		t.Fatalf("NewFiles: %v", err)
	}

	return fset
}
