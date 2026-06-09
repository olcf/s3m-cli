package output

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type Output struct {
	format    Format
	writer    io.Writer
	termWidth int

	protoMessage proto.Message
	tableConfig  *TableConfig
	fields       map[string]any
	pagination   *Pagination
	successMsg   string
}

func NewOutput(format Format, writer io.Writer) *Output {
	width, _ := TerminalDimensions(writer)
	if width == 0 && IsTerminal(writer) {
		width = DefaultTerminalWidth
	}

	return &Output{
		format:    format,
		writer:    writer,
		termWidth: width,
		fields:    make(map[string]any),
	}
}

// Infof prints a message in text mode; ignored in JSON mode.
func (o *Output) Infof(format string, args ...any) {
	if o.format == FormatJSON {
		return
	}

	if _, err := fmt.Fprintf(o.writer, format+"\n", args...); err != nil {
		return
	}
}

func (o *Output) Success(message string) {
	o.successMsg = message
}

func (o *Output) AddField(key string, value any) {
	o.fields[key] = value
}

func (o *Output) SetFields(fields map[string]any) {
	o.fields = fields
}

func (o *Output) SetProtoMessage(msg proto.Message) {
	o.protoMessage = msg
}

func (o *Output) SetPagination(p Pagination) {
	o.pagination = &p
}

func (o *Output) SetTable(config TableConfig) {
	o.tableConfig = &config
}

// SetProtoMessageList extracts a repeated field for JSON output, excluding pagination metadata.
func (o *Output) SetProtoMessageList(msg proto.Message, fieldName string) error {
	if o.format != FormatJSON {
		return nil
	}

	refl := msg.ProtoReflect()
	fields := refl.Descriptor().Fields()
	field := fields.ByName(protoreflect.Name(fieldName))

	if field == nil {
		return fmt.Errorf("field %q not found in message %s", fieldName, refl.Descriptor().FullName())
	}

	if !field.IsList() {
		return fmt.Errorf("field %q is not a repeated field", fieldName)
	}

	list := refl.Get(field).List()
	opts := DefaultJSONOptions()
	jsonMessages := make([]json.RawMessage, list.Len())

	for i := range list.Len() {
		item := list.Get(i).Message().Interface()

		data, err := MarshalProtoJSON(item, opts)
		if err != nil {
			return fmt.Errorf("marshal proto message %d: %w", i, err)
		}

		jsonMessages[i] = data
	}

	o.fields[fieldName] = jsonMessages

	return nil
}

func (o *Output) Render() error {
	switch o.format {
	case FormatJSON:
		return o.renderJSON()
	case FormatText:
		return o.renderText()
	default:
		return o.renderText()
	}
}

func (o *Output) renderJSON() error {
	if o.protoMessage != nil {
		opts := DefaultJSONOptions()

		data, err := MarshalProtoJSON(o.protoMessage, opts)
		if err != nil {
			return fmt.Errorf("marshal proto JSON: %w", err)
		}

		return o.writeJSON(data)
	}

	if len(o.fields) > 0 || o.pagination != nil {
		fields := make(map[string]any, len(o.fields)+1)
		maps.Copy(fields, o.fields)

		if o.pagination != nil {
			fields["pagination"] = o.pagination
		}

		data, err := marshalFields(fields)
		if err != nil {
			return err
		}

		return o.writeJSON(data)
	}

	if o.successMsg != "" {
		result := map[string]any{
			"success": true,
			"message": o.successMsg,
		}

		data, err := marshalFields(result)
		if err != nil {
			return err
		}

		return o.writeJSON(data)
	}

	return nil
}

func (o *Output) renderText() error {
	if o.tableConfig != nil {
		if err := RenderTable(o.writer, *o.tableConfig, o.termWidth); err != nil {
			return err
		}
	}

	if o.pagination != nil {
		if _, err := fmt.Fprintf(o.writer, "%s\n", o.pagination.TextSummary()); err != nil {
			return err
		}
	}

	if o.successMsg != "" {
		if _, err := fmt.Fprintf(o.writer, "✓ %s\n", o.successMsg); err != nil {
			return err
		}
	}

	return nil
}

func marshalFields(fields map[string]any) ([]byte, error) {
	data, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal JSON fields: %w", err)
	}

	return data, nil
}

func (o *Output) writeJSON(data []byte) error {
	if _, err := o.writer.Write(data); err != nil {
		return err
	}

	_, err := o.writer.Write([]byte("\n"))

	return err
}
