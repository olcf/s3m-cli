package output

import (
	"io"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

type Alignment string

const (
	AlignLeft   Alignment = "left"
	AlignCenter Alignment = "center"
	AlignRight  Alignment = "right"
)

type TruncateMode string

const (
	TruncateModeNone   TruncateMode = "none"
	TruncateModeMiddle TruncateMode = "middle"
	TruncateModeEnd    TruncateMode = "end"
)

type TableStyle string

const (
	StyleDefault TableStyle = "default"
	StyleMinimal TableStyle = "minimal"
)

type ColumnConfig struct {
	Name      string
	MaxWidth  int
	MinWidth  int
	Align     Alignment
	Transform func(string) string
	Truncate  TruncateMode
}

type TableConfig struct {
	Headers       []string
	Rows          [][]string
	ColumnConfigs []ColumnConfig
	Style         TableStyle
}

func RenderTable(w io.Writer, config TableConfig, termWidth int) error {
	t := table.NewWriter()
	t.SetOutputMirror(w)

	applyTableStyle(t, config.Style)

	t.AppendHeader(buildHeaderRow(config.Headers))

	colConfigMap := buildColumnConfigMap(config.ColumnConfigs)
	t.SetColumnConfigs(buildColumnConfigs(config.Headers, colConfigMap))

	if termWidth > 0 {
		t.SetAllowedRowLength(termWidth)
	}

	appendRows(t, config, colConfigMap)

	t.Render()

	return nil
}

func applyTableStyle(t table.Writer, style TableStyle) {
	switch style {
	case StyleMinimal:
		t.SetStyle(table.StyleLight)
		t.Style().Options.SeparateRows = false
	case StyleDefault:
		t.SetStyle(table.StyleLight)
	default:
		t.SetStyle(table.StyleLight)
	}
}

func buildHeaderRow(headers []string) table.Row {
	headerRow := make(table.Row, len(headers))
	for i, h := range headers {
		headerRow[i] = h
	}

	return headerRow
}

func buildColumnConfigMap(configs []ColumnConfig) map[string]ColumnConfig {
	colConfigMap := make(map[string]ColumnConfig)
	for _, cc := range configs {
		colConfigMap[cc.Name] = cc
	}

	return colConfigMap
}

func buildColumnConfigs(headers []string, colConfigMap map[string]ColumnConfig) []table.ColumnConfig {
	columnConfigs := make([]table.ColumnConfig, len(headers))
	for i, header := range headers {
		cc := colConfigMap[header]
		colConfig := table.ColumnConfig{
			Number: i + 1,
			Align:  resolveAlignment(cc.Align),
		}

		if cc.MaxWidth > 0 {
			colConfig.WidthMax = cc.MaxWidth
		}

		if cc.MinWidth > 0 {
			colConfig.WidthMin = cc.MinWidth
		}

		columnConfigs[i] = colConfig
	}

	return columnConfigs
}

func resolveAlignment(align Alignment) text.Align {
	switch align {
	case AlignLeft:
		return text.AlignLeft
	case AlignCenter:
		return text.AlignCenter
	case AlignRight:
		return text.AlignRight
	default:
		return text.AlignLeft
	}
}

func appendRows(t table.Writer, config TableConfig, colConfigMap map[string]ColumnConfig) {
	for _, row := range config.Rows {
		tableRow := make(table.Row, len(row))
		for i, cell := range row {
			if i < len(config.Headers) {
				header := config.Headers[i]
				cc := colConfigMap[header]

				if cc.Transform != nil {
					cell = cc.Transform(cell)
				}

				if cc.MaxWidth > 0 {
					cell = truncateString(cell, cc.MaxWidth, cc.Truncate)
				}
			}

			tableRow[i] = cell
		}

		t.AppendRow(tableRow)
	}
}

func truncateString(s string, maxLen int, mode TruncateMode) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}

	switch mode {
	case TruncateModeMiddle:
		if maxLen < 7 {
			// Not enough space for "..."
			return string(runes[:maxLen])
		}

		sideLen := (maxLen - 3) / 2

		return string(runes[:sideLen]) + "..." + string(runes[len(runes)-sideLen:])

	case TruncateModeEnd:
		if maxLen < 4 {
			return string(runes[:maxLen])
		}

		return string(runes[:maxLen-3]) + "..."

	case TruncateModeNone:
		fallthrough
	default:
		// No truncation, let go-pretty handle wrapping
		return s
	}
}

func BuildRow(values ...string) []string {
	return values
}

func BuildRows(rows ...[]string) [][]string {
	return rows
}

func TruncateMiddle(s string, maxLen int) string {
	return truncateString(s, maxLen, TruncateModeMiddle)
}

func TruncateEnd(s string, maxLen int) string {
	return truncateString(s, maxLen, TruncateModeEnd)
}
