package proto

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	commonpb "github.com/olcf/s3m-apis/common/v1alpha"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/olcf/s3m-cli/internal/headermap"
)

type HeaderParam struct {
	Header        string
	ParamName     string
	Required      bool
	AllowedValues []string
}

func ExtractHeadersAndBody(
	rawJSON []byte, headers []HeaderParam,
) (bodyJSON []byte, headerVals map[string]string, err error) {
	if len(headers) == 0 {
		return rawJSON, nil, nil
	}

	var raw map[string]json.RawMessage
	if len(rawJSON) > 0 {
		if err := json.Unmarshal(rawJSON, &raw); err != nil {
			return nil, nil, fmt.Errorf("invalid JSON: %w", err)
		}
	} else {
		raw = map[string]json.RawMessage{}
	}

	headerVals = map[string]string{}

	for _, h := range headers {
		v, ok := raw[h.ParamName]
		if !ok {
			if h.Required {
				return nil, nil, fmt.Errorf("missing required parameter %q", h.ParamName)
			}

			continue
		}

		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return nil, nil, fmt.Errorf("invalid %q: %w", h.ParamName, err)
		}

		if len(h.AllowedValues) > 0 {
			if !slices.Contains(h.AllowedValues, s) {
				return nil, nil, fmt.Errorf("invalid %q: must be one of %v", h.ParamName, h.AllowedValues)
			}
		}

		headerVals[h.Header] = s

		delete(raw, h.ParamName)
	}

	bodyJSON, err = json.Marshal(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request body: %w", err)
	}

	return bodyJSON, headerVals, nil
}

//nolint:gocognit,funlen
func HeaderParamsForMethod(
	enclave string,
	resolver headermap.Resolver,
	sd protoreflect.ServiceDescriptor,
	md protoreflect.MethodDescriptor,
) []HeaderParam {
	var out []HeaderParam

	if resolver == nil {
		resolver = headermap.DefaultResolver()
	}

	enclave = strings.TrimSpace(enclave)
	if enclave == "" {
		enclave = headermap.DefaultEnclave
	}

	if so, ok := sd.Options().(*descriptorpb.ServiceOptions); ok {
		if proto.HasExtension(so, commonpb.E_ServiceHeaderParam) {
			ext := proto.GetExtension(so, commonpb.E_ServiceHeaderParam)
			hs, ok := ext.([]*commonpb.HeaderParam)

			if !ok {
				slog.Warn("service header param extension has unexpected type",
					"service", sd.FullName(), "type", fmt.Sprintf("%T", ext))
			} else {
				for _, h := range hs {
					allowed := resolver.ServiceValues(enclave, string(sd.FullName()), h.GetHeader())

					out = append(out, HeaderParam{
						Header:        h.GetHeader(),
						ParamName:     h.GetParamName(),
						Required:      h.GetRequired(),
						AllowedValues: allowed,
					})
				}
			}
		}
	}

	//nolint:nestif
	if mo, ok := md.Options().(*descriptorpb.MethodOptions); ok {
		if proto.HasExtension(mo, commonpb.E_MethodHeaderParam) {
			ext := proto.GetExtension(mo, commonpb.E_MethodHeaderParam)
			hs, ok := ext.([]*commonpb.HeaderParam)

			if !ok {
				slog.Warn("method header param extension has unexpected type",
					"method", md.FullName(), "type", fmt.Sprintf("%T", ext))
			} else {
				for _, h := range hs {
					allowed := resolver.MethodValues(enclave, string(md.FullName()), h.GetHeader())

					hp := HeaderParam{
						Header:        h.GetHeader(),
						ParamName:     h.GetParamName(),
						Required:      h.GetRequired(),
						AllowedValues: allowed,
					}

					filtered := out[:0]

					for _, existing := range out {
						if existing.ParamName != hp.ParamName {
							filtered = append(filtered, existing)
						}
					}

					filtered = append(filtered, hp)
					out = filtered
				}
			}
		}
	}

	return out
}
