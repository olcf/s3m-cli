package proto

import (
	"slices"
	"testing"

	commonpb "github.com/olcf/s3m-apis/common/v1alpha"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/olcf/s3m-cli/internal/headermap"
)

func TestSchemaForMessageBuildsPropertiesAndOneOf(t *testing.T) {
	msg := &descriptorpb.DescriptorProto{
		Name: proto.String("Widget"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{Name: proto.String("name"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
			{Name: proto.String("count"), Number: proto.Int32(2), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum()},
			{Name: proto.String("tags"), Number: proto.Int32(3), Label: descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
			{
				Name:     proto.String("labels"),
				Number:   proto.Int32(4),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
				TypeName: proto.String(".test.Widget.LabelsEntry"),
			},
			{Name: proto.String("updated_at"), Number: proto.Int32(5), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(), TypeName: proto.String(".google.protobuf.Timestamp")},
		},
		NestedType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("LabelsEntry"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("key"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
					{Name: proto.String("value"), Number: proto.Int32(2), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum()},
				},
				Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
			},
		},
		OneofDecl: []*descriptorpb.OneofDescriptorProto{{Name: proto.String("choice")}},
	}
	// oneof fields
	msg.Field = append(msg.Field,
		&descriptorpb.FieldDescriptorProto{
			Name:       proto.String("cat"),
			Number:     proto.Int32(6),
			Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:       descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
			OneofIndex: proto.Int32(0),
		},
		&descriptorpb.FieldDescriptorProto{
			Name:       proto.String("dog"),
			Number:     proto.Int32(7),
			Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:       descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
			OneofIndex: proto.Int32(0),
		},
	)

	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("widget.proto"),
		Package: proto.String("test"),
		Syntax:  proto.String("proto3"),
		Dependency: []string{
			timestamppb.File_google_protobuf_timestamp_proto.Path(),
		},
		MessageType: []*descriptorpb.DescriptorProto{msg},
	}

	fd := buildTestFileDescriptor(
		t,
		fdp,
		protodesc.ToFileDescriptorProto(timestamppb.File_google_protobuf_timestamp_proto),
	)
	md := fd.Messages().ByName("Widget")
	schema := SchemaForMessage(md)

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing")
	}
	if _, ok := props["labels"]; !ok {
		t.Fatalf("expected map field properties")
	}
	if updated, ok := props["updatedAt"].(map[string]any); !ok || updated["format"] != "date-time" {
		t.Fatalf("expected well-known type handling, got %v", updated)
	}

	if required, ok := schema["required"].([]string); ok && len(required) > 0 {
		t.Fatalf("expected no implicit proto3 required fields, got %v", required)
	}

	if _, ok := schema["oneOf"]; ok {
		t.Fatalf("expected oneof constraints to avoid exact-one semantics, got %v", schema["oneOf"])
	}

	allOf, ok := schema["allOf"].([]any)
	if !ok || len(allOf) != 1 {
		t.Fatalf("expected one pairwise exclusion constraint, got %v", schema["allOf"])
	}

	clause, ok := allOf[0].(map[string]any)
	if !ok {
		t.Fatalf("expected oneof clause map, got %T", allOf[0])
	}

	notClause, ok := clause["not"].(map[string]any)
	if !ok {
		t.Fatalf("expected oneof not clause, got %v", clause)
	}

	pair, ok := notClause["allOf"].([]any)
	if !ok || len(pair) != 2 {
		t.Fatalf("expected pairwise allOf constraint, got %v", notClause["allOf"])
	}

	requiredNames := make(map[string]struct{}, len(pair))
	for _, item := range pair {
		entry, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected required entry map, got %T", item)
		}

		names, ok := entry["required"].([]string)
		if !ok || len(names) != 1 {
			t.Fatalf("expected single required field, got %v", entry["required"])
		}

		requiredNames[names[0]] = struct{}{}
	}

	if _, ok := requiredNames["cat"]; !ok {
		t.Fatalf("expected cat in pairwise constraint, got %v", requiredNames)
	}

	if _, ok := requiredNames["dog"]; !ok {
		t.Fatalf("expected dog in pairwise constraint, got %v", requiredNames)
	}
}

func TestSchemaForMessagePreservesNestedConstraints(t *testing.T) {
	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("nested_schema.proto"),
		Package: proto.String("test.v1"),
		Syntax:  proto.String("proto2"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Inner"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("must_set"),
						JsonName: proto.String("mustSet"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REQUIRED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
					{
						Name:       proto.String("cat"),
						Number:     proto.Int32(2),
						Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:       descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						OneofIndex: proto.Int32(0),
					},
					{
						Name:       proto.String("dog"),
						Number:     proto.Int32(3),
						Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:       descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						OneofIndex: proto.Int32(0),
					},
				},
				OneofDecl: []*descriptorpb.OneofDescriptorProto{{Name: proto.String("choice")}},
			},
			{
				Name: proto.String("Outer"),
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name:     proto.String("inner"),
					JsonName: proto.String("inner"),
					Number:   proto.Int32(1),
					Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
					TypeName: proto.String(".test.v1.Inner"),
				}},
			},
		},
	}

	fd := buildTestFileDescriptor(t, fdp)
	schema := SchemaForMessage(fd.Messages().ByName("Outer"))
	props := schema["properties"].(map[string]any)
	inner, ok := props["inner"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested object schema, got %v", props["inner"])
	}

	required, ok := inner["required"].([]string)
	if !ok || !slices.Contains(required, "mustSet") {
		t.Fatalf("expected nested required field, got %v", inner["required"])
	}

	allOf, ok := inner["allOf"].([]any)
	if !ok || len(allOf) != 1 {
		t.Fatalf("expected nested oneof constraint, got %v", inner["allOf"])
	}
}

func TestSchemaForMethodAddsHeaderParams(t *testing.T) {
	so := &descriptorpb.ServiceOptions{}
	proto.SetExtension(so, commonpb.E_ServiceHeaderParam, []*commonpb.HeaderParam{
		{Header: "olcf-resource", ParamName: "resource", Required: *proto.Bool(true)},
	})

	msg := &descriptorpb.DescriptorProto{
		Name: proto.String("Input"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{Name: proto.String("payload"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
		},
	}

	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("headers.proto"),
		Package: proto.String("test"),
		Syntax:  proto.String("proto3"),
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name:    proto.String("Tools"),
			Options: so,
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       proto.String("Run"),
				InputType:  proto.String(".test.Input"),
				OutputType: proto.String(".test.Input"),
			}},
		}},
		MessageType: []*descriptorpb.DescriptorProto{msg},
	}

	fd := buildTestFileDescriptor(t, fdp)
	sd := fd.Services().ByName("Tools")
	mdesc := sd.Methods().ByName("Run")

	resolver := headermap.NewStaticResolver(
		map[string]map[string]map[string][]string{
			headermap.DefaultEnclave: {
				"test.Tools": {"olcf-resource": {"alpha", "beta"}},
			},
		},
		nil,
	)

	headers := HeaderParamsForMethod(headermap.DefaultEnclave, resolver, sd, mdesc)

	schema := SchemaForMethod(mdesc, headers)
	props := schema["properties"].(map[string]any)
	headerProp, ok := props["resource"].(map[string]any)
	if !ok {
		t.Fatalf("header param property missing: %v", props)
	}
	if enum, ok := headerProp["enum"].([]string); !ok || len(enum) == 0 {
		t.Fatalf("expected allowed values propagated, got %v", headerProp)
	}

	required := schema["required"].([]string)
	if !slices.Contains(required, "resource") {
		t.Fatalf("header param not marked required: %v", required)
	}
}
