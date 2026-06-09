package proto

import (
	"testing"

	commonpb "github.com/olcf/s3m-apis/common/v1alpha"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/olcf/s3m-cli/internal/headermap"
)

func TestExtractHeadersAndBodyValidation(t *testing.T) {
	headers := []HeaderParam{{
		Header:        "olcf-resource",
		ParamName:     "compute_resource",
		Required:      true,
		AllowedValues: []string{"defiant", "quokka"},
	}}

	body, hdrs, err := ExtractHeadersAndBody([]byte(`{"compute_resource":"defiant","foo":"bar"}`), headers)
	if err != nil {
		t.Fatalf("ExtractHeadersAndBody error: %v", err)
	}
	if hdrs["olcf-resource"] != "defiant" {
		t.Fatalf("expected header value, got %v", hdrs)
	}
	if string(body) != `{"foo":"bar"}` {
		t.Fatalf("unexpected body: %s", string(body))
	}

	if _, _, err := ExtractHeadersAndBody([]byte(`{"compute_resource":"invalid"}`), headers); err == nil {
		t.Fatal("expected error for invalid header value")
	}

	if _, _, err := ExtractHeadersAndBody([]byte(`{"foo":"bar"}`), headers); err == nil {
		t.Fatal("expected error for missing required header")
	}
}

func TestHeaderParamsForMethodUsesResolver(t *testing.T) {
	so := &descriptorpb.ServiceOptions{}
	proto.SetExtension(so, commonpb.E_ServiceHeaderParam, []*commonpb.HeaderParam{
		{Header: "x-svc", ParamName: "hdr", Required: *proto.Bool(true)},
	})

	mo := &descriptorpb.MethodOptions{}
	proto.SetExtension(mo, commonpb.E_MethodHeaderParam, []*commonpb.HeaderParam{
		{Header: "x-svc", ParamName: "hdr", Required: *proto.Bool(true)},
	})

	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("resolver.proto"),
		Package: proto.String("test"),
		Syntax:  proto.String("proto3"),
		Dependency: []string{
			emptypb.File_google_protobuf_empty_proto.Path(),
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name:    proto.String("Svc"),
			Options: so,
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       proto.String("Call"),
				InputType:  proto.String(".google.protobuf.Empty"),
				OutputType: proto.String(".google.protobuf.Empty"),
				Options:    mo,
			}},
		}},
	}

	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			fdp,
			protodesc.ToFileDescriptorProto(emptypb.File_google_protobuf_empty_proto),
		},
	})
	if err != nil {
		t.Fatalf("NewFiles: %v", err)
	}

	fd, err := files.FindFileByPath("resolver.proto")
	if err != nil {
		t.Fatalf("FindFileByPath: %v", err)
	}

	sd := fd.Services().ByName("Svc")
	md := sd.Methods().ByName("Call")

	resolver := headermap.NewStaticResolver(
		map[string]map[string]map[string][]string{
			headermap.DefaultEnclave: {"test.Svc": {"x-svc": {"svc"}}},
		},
		map[string]map[string]map[string][]string{
			headermap.DefaultEnclave: {"test.Svc.Call": {"x-svc": {"method"}}},
		},
	)

	hdrs := HeaderParamsForMethod(headermap.DefaultEnclave, resolver, sd, md)
	if len(hdrs) != 1 {
		t.Fatalf("expected single header param, got %d", len(hdrs))
	}
	if hdrs[0].AllowedValues == nil || len(hdrs[0].AllowedValues) != 1 || hdrs[0].AllowedValues[0] != "method" {
		t.Fatalf("expected method-level allowed values, got %+v", hdrs[0])
	}
	if !hdrs[0].Required {
		t.Fatal("expected header param to be required")
	}
}
