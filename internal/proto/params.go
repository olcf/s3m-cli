package proto

import "google.golang.org/protobuf/reflect/protoreflect"

type ParamSource string

const (
	ParamSourceHeader ParamSource = "header"
	ParamSourceBody   ParamSource = "body"
)

type ParamSpec struct {
	Name          string
	Required      bool
	HasDefault    bool
	Source        ParamSource
	AllowedValues []string
}

func ParamsForMethod(md protoreflect.MethodDescriptor, headers []HeaderParam) []ParamSpec {
	var (
		requiredNoDefault   []ParamSpec
		requiredWithDefault []ParamSpec
		optional            []ParamSpec
	)

	appendParam := func(p ParamSpec) {
		switch {
		case p.Required && !p.HasDefault:
			requiredNoDefault = append(requiredNoDefault, p)
		case p.Required && p.HasDefault:
			requiredWithDefault = append(requiredWithDefault, p)
		default:
			optional = append(optional, p)
		}
	}

	for _, h := range headers {
		appendParam(ParamSpec{
			Name:          h.ParamName,
			Required:      h.Required,
			HasDefault:    false,
			Source:        ParamSourceHeader,
			AllowedValues: h.AllowedValues,
		})
	}

	visitFields(md.Input(), "", true, appendParam, map[protoreflect.FullName]struct{}{})

	out := make([]ParamSpec, 0, len(requiredNoDefault)+len(requiredWithDefault)+len(optional))
	out = append(out, requiredNoDefault...)
	out = append(out, requiredWithDefault...)
	out = append(out, optional...)

	return out
}

func isFieldRequired(fd protoreflect.FieldDescriptor) bool {
	if fd == nil {
		return false
	}

	if od := fd.ContainingOneof(); od != nil && !od.IsSynthetic() {
		return false
	}

	return fd.Cardinality() == protoreflect.Required
}

// visitFields walks the message fields, flattening nested messages into dotted
// parameter paths like "job.account".
func visitFields(
	msg protoreflect.MessageDescriptor,
	prefix string,
	parentRequired bool,
	appendParam func(ParamSpec),
	seen map[protoreflect.FullName]struct{},
) {
	if msg == nil {
		return
	}

	if _, ok := seen[msg.FullName()]; ok {
		// Prevent infinite recursion for self-referential messages.
		return
	}

	seen[msg.FullName()] = struct{}{}
	defer delete(seen, msg.FullName())

	fields := msg.Fields()
	for i := range fields.Len() {
		fd := fields.Get(i)
		if fd == nil {
			continue
		}

		name := fd.JSONName()
		if prefix != "" {
			name = prefix + "." + name
		}

		required := parentRequired && isFieldRequired(fd)

		if fd.Kind() == protoreflect.MessageKind && !fd.IsMap() && fd.Cardinality() != protoreflect.Repeated {
			visitFields(fd.Message(), name, required, appendParam, seen)

			continue
		}

		appendParam(ParamSpec{
			Name:       name,
			Required:   required,
			HasDefault: fd.HasDefault(),
			Source:     ParamSourceBody,
		})
	}
}
