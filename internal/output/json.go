package output

import (
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type JSONOptions struct {
	Indent          bool
	EmitUnpopulated bool
	UseProtoNames   bool
}

func DefaultJSONOptions() JSONOptions {
	return JSONOptions{
		Indent:          true,
		EmitUnpopulated: true,
		UseProtoNames:   false,
	}
}

func MarshalProtoJSON(msg proto.Message, opts JSONOptions) ([]byte, error) {
	m := protojson.MarshalOptions{
		EmitUnpopulated: opts.EmitUnpopulated,
		UseProtoNames:   opts.UseProtoNames,
	}

	if opts.Indent {
		m.Indent = "  "
	}

	return m.Marshal(msg)
}
