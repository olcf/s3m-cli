package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		mode     TruncateMode
		expected string
	}{
		{"no truncate needed", "hello", 10, TruncateModeEnd, "hello"},
		{"truncate end", "hello world", 8, TruncateModeEnd, "hello..."},
		{"truncate middle", "hello world test", 10, TruncateModeMiddle, "hel...est"},
		{"truncate middle even", "hello world", 8, TruncateModeMiddle, "he...ld"},
		{"no truncate mode", "hello world", 8, TruncateModeNone, "hello world"},
		{"empty string", "", 5, TruncateModeEnd, ""},
		{"exact length", "hello", 5, TruncateModeEnd, "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateString(tt.input, tt.maxLen, tt.mode)
			if result != tt.expected {
				t.Errorf("truncateString(%q, %d, %v) = %q, want %q",
					tt.input, tt.maxLen, tt.mode, result, tt.expected)
			}
		})
	}
}

func TestTruncateMiddle(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{"path", "/very/long/path/to/some/file.txt", 20, "/very/lo...file.txt"},
		{"short", "short", 20, "short"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TruncateMiddle(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("TruncateMiddle(%q, %d) = %q, want %q",
					tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestTruncateEnd(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{"long string", "this is a very long string", 15, "this is a ve..."},
		{"short string", "short", 20, "short"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TruncateEnd(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("TruncateEnd(%q, %d) = %q, want %q",
					tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestBuildRow(t *testing.T) {
	result := BuildRow("a", "b", "c")
	expected := []string{"a", "b", "c"}

	if len(result) != len(expected) {
		t.Errorf("BuildRow() length = %d, want %d", len(result), len(expected))
	}

	for i := range result {
		if result[i] != expected[i] {
			t.Errorf("BuildRow()[%d] = %s, want %s", i, result[i], expected[i])
		}
	}
}

func TestRenderTable_AppliesColumnConfigs(t *testing.T) {
	var buf bytes.Buffer

	err := RenderTable(&buf, TableConfig{
		Headers: []string{"NAME", "COUNT"},
		Rows: [][]string{
			{"alpha beta gamma", "7"},
		},
		ColumnConfigs: []ColumnConfig{
			{
				Name:      "NAME",
				MaxWidth:  8,
				Truncate:  TruncateModeEnd,
				Transform: strings.ToUpper,
			},
			{
				Name:      "COUNT",
				Transform: func(s string) string { return "[" + s + "]" },
			},
		},
	}, 0)
	if err != nil {
		t.Fatalf("RenderTable() error: %v", err)
	}

	result := buf.String()
	if !strings.Contains(result, "ALPHA...") {
		t.Fatalf("expected transformed and truncated NAME column in rendered table, got: %s", result)
	}
	if strings.Contains(result, "alpha beta gamma") {
		t.Fatalf("expected NAME column config to affect rendered output, got: %s", result)
	}
	if !strings.Contains(result, "[7]") {
		t.Fatalf("expected COUNT column transform to affect rendered output, got: %s", result)
	}
}
