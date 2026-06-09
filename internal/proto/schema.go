package proto

import (
	"log/slog"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/olcf/s3m-cli/internal/util"
)

const MaxSchemaDepth = 4

//
// Schema generation

func SchemaForMethod(md protoreflect.MethodDescriptor, headers []HeaderParam) map[string]any {
	schema := SchemaForMessage(md.Input())

	if len(headers) == 0 {
		return schema
	}

	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
		schema["properties"] = props
	}

	var required []string
	if r, ok := schema["required"].([]string); ok {
		required = r
	}

	for _, h := range headers {
		prop := map[string]any{
			"type":        "string",
			"description": "Value for " + h.Header + " header",
		}
		if len(h.AllowedValues) > 0 {
			prop["enum"] = h.AllowedValues
		}

		props[h.ParamName] = prop

		if h.Required {
			required = append(required, h.ParamName)
		}
	}

	if len(required) > 0 {
		schema["required"] = required
	}

	return schema
}

func SchemaForMessage(md protoreflect.MessageDescriptor) map[string]any {
	return schemaForMessage(md, 0)
}

func schemaForMessage(md protoreflect.MessageDescriptor, depth int) map[string]any {
	props := map[string]any{}
	fields := md.Fields()
	required := make([]string, 0, fields.Len())
	oneofGroups := map[protoreflect.OneofDescriptor][]string{}

	for i := range fields.Len() {
		f := fields.Get(i)
		props[f.JSONName()] = schemaForField(f, depth)

		if od := f.ContainingOneof(); od != nil && !od.IsSynthetic() {
			oneofGroups[od] = append(oneofGroups[od], f.JSONName())
		} else if isFieldRequired(f) {
			required = append(required, f.JSONName())
		}
	}

	schema := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}

	if len(required) > 0 {
		schema["required"] = util.DedupeStrings(required)
	}

	if constraints := oneofConstraints(md, oneofGroups); len(constraints) > 0 {
		schema["allOf"] = constraints
	}

	return schema
}

func schemaForField(fd protoreflect.FieldDescriptor, depth int) map[string]any {
	if fd.IsList() {
		return map[string]any{
			"type":  "array",
			"items": schemaForScalarOrMsg(fd, depth),
		}
	}

	if fd.IsMap() {
		val := fd.MapValue()

		if val.Kind() == protoreflect.EnumKind {
			return map[string]any{
				"type":                 "object",
				"additionalProperties": schemaForEnum(val.Enum()),
			}
		}

		return map[string]any{
			"type":                 "object",
			"additionalProperties": schemaForKind(val.Kind(), val.Message(), depth),
		}
	}

	return schemaForScalarOrMsg(fd, depth)
}

func schemaForScalarOrMsg(fd protoreflect.FieldDescriptor, depth int) map[string]any {
	if fd.Kind() == protoreflect.EnumKind {
		return schemaForEnum(fd.Enum())
	}

	return schemaForKind(fd.Kind(), fd.Message(), depth)
}

func schemaForKind(k protoreflect.Kind, msg protoreflect.MessageDescriptor, depth int) map[string]any {
	switch k {
	case protoreflect.BoolKind:
		return t("boolean")
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return t("integer")
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return t("string")
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return t("number")
	case protoreflect.StringKind:
		return t("string")
	case protoreflect.BytesKind:
		return with(t("string"), "contentEncoding", "base64")
	case protoreflect.EnumKind:
		return t("string")
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if msg == nil {
			return t("object")
		}

		if schema, ok := schemaForWellKnownType(msg); ok {
			return schema
		}

		if depth > MaxSchemaDepth {
			slog.Warn("avoiding infinite recursion for recursive message", "message", msg.FullName(), "depth", depth)

			return t("object")
		}

		return schemaForMessage(msg, depth+1)
	default:
		return t("object")
	}
}

func schemaForEnum(ed protoreflect.EnumDescriptor) map[string]any {
	vals := ed.Values()
	names := make([]string, vals.Len())
	numbers := make([]int32, vals.Len())

	for i := range vals.Len() {
		names[i] = string(vals.Get(i).Name())
		numbers[i] = int32(vals.Get(i).Number())
	}

	return map[string]any{
		"oneOf": []any{
			map[string]any{"type": "string", "enum": names},
			map[string]any{"type": "integer", "enum": numbers},
		},
	}
}

//
// Utilities

func t(kind string) map[string]any                          { return map[string]any{"type": kind} }
func with(m map[string]any, k string, v any) map[string]any { m[k] = v; return m }

func oneofConstraints(
	md protoreflect.MessageDescriptor, groups map[protoreflect.OneofDescriptor][]string,
) []any {
	oneofs := md.Oneofs()
	constraints := make([]any, 0)

	for i := range oneofs.Len() {
		fields := groups[oneofs.Get(i)]
		if len(fields) < 2 {
			continue
		}

		for left := range fields {
			for right := left + 1; right < len(fields); right++ {
				constraints = append(constraints, map[string]any{
					"not": map[string]any{
						"allOf": []any{
							map[string]any{"required": []string{fields[left]}},
							map[string]any{"required": []string{fields[right]}},
						},
					},
				})
			}
		}
	}

	return constraints
}

//
// Well-known type handling

func schemaForWellKnownType(msg protoreflect.MessageDescriptor) (schema map[string]any, ok bool) {
	switch string(msg.FullName()) {
	case "google.protobuf.Timestamp":
		return with(t("string"), "format", "date-time"), true
	case "google.protobuf.Duration":
		return t("string"), true
	case "google.protobuf.Struct":
		return t("object"), true
	case "google.protobuf.FieldMask":
		return t("string"), true
	case "google.protobuf.ListValue":
		return map[string]any{"type": "array"}, true
	case "google.protobuf.Value":
		return map[string]any{"type": []any{"object", "array", "string", "number", "integer", "boolean", "null"}}, true
	case "google.protobuf.Any":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"@type": t("string"),
				"value": map[string]any{},
			},
			"required":             []string{"@type"},
			"additionalProperties": true,
		}, true
	case "google.protobuf.Int32Value", "google.protobuf.UInt32Value":
		return t("integer"), true
	case "google.protobuf.Int64Value", "google.protobuf.UInt64Value":
		return t("string"), true
	case "google.protobuf.BoolValue":
		return t("boolean"), true
	case "google.protobuf.StringValue":
		return t("string"), true
	case "google.protobuf.BytesValue":
		return with(t("string"), "contentEncoding", "base64"), true
	case "google.protobuf.FloatValue", "google.protobuf.DoubleValue":
		return t("number"), true
	default:
		return nil, false
	}
}
