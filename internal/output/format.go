package output

import (
	"errors"
	"fmt"
	"io"
	"time"

	"google.golang.org/protobuf/proto"
)

type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

type Formatter struct {
	format     Format
	writer     io.Writer
	termWidth  int
	termHeight int
}

func NewFormatter(format Format, writer io.Writer) *Formatter {
	width, height := TerminalDimensions(writer)

	if width == 0 {
		width = DefaultTerminalWidth
	}

	if height == 0 {
		height = DefaultTerminalHeight
	}

	return &Formatter{
		format:     format,
		writer:     writer,
		termWidth:  width,
		termHeight: height,
	}
}

func (f *Formatter) RenderTable(config TableConfig) error {
	if f.format == FormatJSON {
		return errors.New("cannot render table in JSON format; use RenderProtoJSON or RenderJSON instead")
	}

	return RenderTable(f.writer, config, f.termWidth)
}

func (f *Formatter) RenderProtoJSON(msg proto.Message) error {
	opts := DefaultJSONOptions()

	data, err := MarshalProtoJSON(msg, opts)
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}

	_, err = f.writer.Write(data)
	if err != nil {
		return fmt.Errorf("write JSON: %w", err)
	}

	_, err = f.writer.Write([]byte("\n"))

	return err
}

func (f *Formatter) RenderJSON(data []byte) error {
	_, err := f.writer.Write(data)
	if err != nil {
		return fmt.Errorf("write JSON: %w", err)
	}

	_, err = f.writer.Write([]byte("\n"))

	return err
}

func (f *Formatter) Write(text string) error {
	_, err := f.writer.Write([]byte(text))
	return err
}

func (f *Formatter) Writeln(text string) error {
	_, err := f.writer.Write([]byte(text + "\n"))
	return err
}

func (f *Formatter) Printf(format string, args ...any) error {
	_, err := fmt.Fprintf(f.writer, format, args...)
	return err
}

func (f *Formatter) IsJSON() bool {
	return f.format == FormatJSON
}

func (f *Formatter) GetFormat() Format {
	return f.format
}

//
// Helper functions

func FormatBytes(b int64) string {
	const unit = 1024

	if b < unit {
		return fmt.Sprintf("%d B", b)
	}

	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func ShortID(id string) string {
	if len(id) <= 8 {
		return id
	}

	return id[:8]
}

func FormatTimestamp(t time.Time) string {
	return t.Format("2006-01-02 15:04")
}

func FormatTimestampISO(t time.Time) string {
	return t.Format(time.RFC3339)
}

func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}

	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}

	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}

	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
