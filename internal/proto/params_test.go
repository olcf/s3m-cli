package proto

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestParamsForMethodOrdersByRequirement(t *testing.T) {
	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("params.proto"),
		Package: proto.String("test.v1"),
		Syntax:  proto.String("proto2"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Input"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{
					Name:     proto.String("must_set"),
					JsonName: proto.String("must_set"),
					Number:   proto.Int32(1),
					Label:    descriptorpb.FieldDescriptorProto_LABEL_REQUIRED.Enum(),
					Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				},
				{
					Name:         proto.String("req_with_default"),
					JsonName:     proto.String("req_with_default"),
					Number:       proto.Int32(2),
					Label:        descriptorpb.FieldDescriptorProto_LABEL_REQUIRED.Enum(),
					Type:         descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					DefaultValue: proto.String("fallback"),
				},
				{
					Name:         proto.String("opt_with_default"),
					JsonName:     proto.String("opt_with_default"),
					Number:       proto.Int32(3),
					Label:        descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:         descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					DefaultValue: proto.String("opt"),
				},
				{
					Name:     proto.String("optional_field"),
					JsonName: proto.String("optional_field"),
					Number:   proto.Int32(4),
					Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:     descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
				},
			},
		}},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("Svc"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       proto.String("Call"),
				InputType:  proto.String(".test.v1.Input"),
				OutputType: proto.String(".test.v1.Input"),
			}},
		}},
	}

	fd := buildTestFileDescriptor(t, fdp)
	sd := fd.Services().ByName("Svc")
	md := sd.Methods().ByName("Call")

	headers := []HeaderParam{{
		Header:    "x-test",
		ParamName: "compute_resource",
		Required:  true,
	}}

	params := ParamsForMethod(md, headers)

	wantOrder := []string{
		"compute_resource",
		"must_set",
		"req_with_default",
		"opt_with_default",
		"optional_field",
	}

	if len(params) != len(wantOrder) {
		t.Fatalf("expected %d params, got %d: %+v", len(wantOrder), len(params), params)
	}

	for i, name := range wantOrder {
		if params[i].Name != name {
			t.Fatalf("param %d: expected %q, got %q", i, name, params[i].Name)
		}
	}

	if !params[0].Required || params[0].Source != ParamSourceHeader {
		t.Fatalf("expected header param required, got %+v", params[0])
	}
	if !params[1].Required || params[1].HasDefault {
		t.Fatalf("expected required without default: %+v", params[1])
	}
	if !params[2].Required || !params[2].HasDefault {
		t.Fatalf("expected required with default: %+v", params[2])
	}
	if params[3].Required {
		t.Fatalf("expected optional param: %+v", params[3])
	}
}

func TestParamsForMethodIncludesNestedFields(t *testing.T) {
	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("nested_params.proto"),
		Package: proto.String("test.v1"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Job"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("account"),
						JsonName: proto.String("account"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
					{
						Name:     proto.String("script"),
						JsonName: proto.String("script"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
				},
			},
			{
				Name: proto.String("Input"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("job"),
						JsonName: proto.String("job"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".test.v1.Job"),
					},
				},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("Svc"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       proto.String("Call"),
				InputType:  proto.String(".test.v1.Input"),
				OutputType: proto.String(".test.v1.Input"),
			}},
		}},
	}

	fd := buildTestFileDescriptor(t, fdp)
	sd := fd.Services().ByName("Svc")
	md := sd.Methods().ByName("Call")

	params := ParamsForMethod(md, nil)

	want := map[string]bool{
		"job.account": true,
		"job.script":  true,
	}

	for _, p := range params {
		delete(want, p.Name)
	}

	if len(want) > 0 {
		t.Fatalf("missing nested params: %+v", want)
	}
}

func TestParamsForMethodLeavesImplicitProto3FieldsOptional(t *testing.T) {
	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("implicit_params.proto"),
		Package: proto.String("test.v1"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Input"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{
					Name:     proto.String("name"),
					JsonName: proto.String("name"),
					Number:   proto.Int32(1),
					Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				},
				{
					Name:     proto.String("tags"),
					JsonName: proto.String("tags"),
					Number:   proto.Int32(2),
					Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
					Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				},
				{
					Name:     proto.String("labels"),
					JsonName: proto.String("labels"),
					Number:   proto.Int32(3),
					Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
					Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
					TypeName: proto.String(".test.v1.Input.LabelsEntry"),
				},
			},
			NestedType: []*descriptorpb.DescriptorProto{{
				Name: proto.String("LabelsEntry"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("key"),
						Number: proto.Int32(1),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
					{
						Name:   proto.String("value"),
						Number: proto.Int32(2),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
				},
				Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
			}},
		}},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("Svc"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       proto.String("Call"),
				InputType:  proto.String(".test.v1.Input"),
				OutputType: proto.String(".test.v1.Input"),
			}},
		}},
	}

	fd := buildTestFileDescriptor(t, fdp)
	params := ParamsForMethod(fd.Services().ByName("Svc").Methods().ByName("Call"), nil)

	seen := make(map[string]ParamSpec, len(params))
	for _, param := range params {
		seen[param.Name] = param
	}

	for _, name := range []string{"name", "tags", "labels"} {
		param, ok := seen[name]
		if !ok {
			t.Fatalf("missing param %q in %+v", name, params)
		}

		if param.Required {
			t.Fatalf("expected proto3 param %q to remain optional, got %+v", name, param)
		}
	}
}
