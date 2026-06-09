package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
)

func TestOutput_Info_TextMode(t *testing.T) {
	buf := &bytes.Buffer{}
	out := NewOutput(FormatText, buf)

	out.Infof("Processing files...")
	out.Infof("Found %d items", 5)

	result := buf.String()
	if !strings.Contains(result, "Processing files...") {
		t.Errorf("Expected info message in text mode, got: %s", result)
	}
	if !strings.Contains(result, "Found 5 items") {
		t.Errorf("Expected formatted info message in text mode, got: %s", result)
	}
}

func TestOutput_Info_JSONMode(t *testing.T) {
	buf := &bytes.Buffer{}
	out := NewOutput(FormatJSON, buf)

	out.Infof("Processing files...")
	out.Infof("Found %d items", 5)

	result := buf.String()
	if result != "" {
		t.Errorf("Expected no output from Info() in JSON mode, got: %s", result)
	}
}

func TestOutput_Success_TextMode(t *testing.T) {
	buf := &bytes.Buffer{}
	out := NewOutput(FormatText, buf)

	out.Success("Operation completed")
	err := out.Render()

	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	result := buf.String()
	if !strings.Contains(result, "✓ Operation completed") {
		t.Errorf("Expected success message with checkmark, got: %s", result)
	}
}

func TestOutput_AddField_JSONMode(t *testing.T) {
	buf := &bytes.Buffer{}
	out := NewOutput(FormatJSON, buf)

	out.AddField("key1", "value1")
	out.AddField("key2", 42)
	out.AddField("key3", true)
	err := out.Render()

	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	result := buf.String()
	if !strings.Contains(result, `"key1"`) || !strings.Contains(result, `"value1"`) {
		t.Errorf("Expected key1/value1 in JSON output, got: %s", result)
	}
	if !strings.Contains(result, `"key2"`) || !strings.Contains(result, `42`) {
		t.Errorf("Expected key2/42 in JSON output, got: %s", result)
	}
	if !strings.Contains(result, `"key3"`) || !strings.Contains(result, `true`) {
		t.Errorf("Expected key3/true in JSON output, got: %s", result)
	}
}

func TestOutput_AddField_TextMode(t *testing.T) {
	buf := &bytes.Buffer{}
	out := NewOutput(FormatText, buf)

	out.AddField("key1", "value1")
	out.AddField("key2", 42)
	err := out.Render()

	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	result := buf.String()
	// Fields should be ignored in text mode without Success
	if result != "" {
		t.Errorf("Expected no output from AddField() without Success in text mode, got: %s", result)
	}
}

func TestOutput_SetFields(t *testing.T) {
	buf := &bytes.Buffer{}
	out := NewOutput(FormatJSON, buf)

	fields := map[string]interface{}{
		"field1": "value1",
		"field2": 123,
		"field3": []string{"a", "b", "c"},
	}
	out.SetFields(fields)
	err := out.Render()

	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	result := buf.String()
	if !strings.Contains(result, `"field1"`) {
		t.Errorf("Expected field1 in JSON output, got: %s", result)
	}
	if !strings.Contains(result, `"field2"`) {
		t.Errorf("Expected field2 in JSON output, got: %s", result)
	}
	if !strings.Contains(result, `"field3"`) {
		t.Errorf("Expected field3 in JSON output, got: %s", result)
	}
}

func TestOutput_Pagination_JSONMode(t *testing.T) {
	buf := &bytes.Buffer{}
	out := NewOutput(FormatJSON, buf)

	out.AddField("datasets", []string{"one"})
	out.SetPagination(Pagination{
		Limit:         10,
		Offset:        5,
		Returned:      1,
		NextOffset:    6,
		HasMore:       true,
		NextPageToken: "offset:6",
	})

	err := out.Render()
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	result := buf.String()
	if !strings.Contains(result, `"pagination"`) {
		t.Errorf("Expected pagination field in JSON output, got: %s", result)
	}
	if !strings.Contains(result, `"nextPageToken"`) {
		t.Errorf("Expected nextPageToken in JSON output, got: %s", result)
	}
}

func TestOutput_Success_WithFields_JSONMode(t *testing.T) {
	buf := &bytes.Buffer{}
	out := NewOutput(FormatJSON, buf)

	out.Success("Operation succeeded")
	out.AddField("status", "completed")
	out.AddField("count", 5)
	err := out.Render()

	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	result := buf.String()
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal JSON output: %v", err)
	}

	if decoded["status"] != "completed" {
		t.Errorf("expected status field to win JSON precedence, got: %v", decoded["status"])
	}
	if decoded["count"] != float64(5) {
		t.Errorf("expected count field to survive JSON precedence, got: %v", decoded["count"])
	}
	if _, ok := decoded["success"]; ok {
		t.Errorf("expected success envelope to stay absent when fields are present, got: %s", result)
	}
	if _, ok := decoded["message"]; ok {
		t.Errorf("expected success message to stay absent when fields are present, got: %s", result)
	}
}

func TestOutput_Table_TextMode(t *testing.T) {
	buf := &bytes.Buffer{}
	out := NewOutput(FormatText, buf)

	table := TableConfig{
		Headers: []string{"NAME", "COUNT"},
		Rows: [][]string{
			{"item1", "10"},
			{"item2", "20"},
		},
	}
	out.SetTable(table)
	err := out.Render()

	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	result := buf.String()
	if !strings.Contains(result, "NAME") || !strings.Contains(result, "COUNT") {
		t.Errorf("Expected table headers in text mode, got: %s", result)
	}
	if !strings.Contains(result, "item1") || !strings.Contains(result, "item2") {
		t.Errorf("Expected table rows in text mode, got: %s", result)
	}
}

func TestOutput_Table_TextMode_NonTerminalDoesNotHardClip(t *testing.T) {
	longValue := strings.Repeat("x", DefaultTerminalWidth+20)

	buf := &bytes.Buffer{}
	out := NewOutput(FormatText, buf)
	out.SetTable(TableConfig{
		Headers: []string{"VALUE", "MARKER"},
		Rows: [][]string{
			{longValue, "tail"},
		},
	})

	if err := out.Render(); err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	result := buf.String()
	if !strings.Contains(result, longValue) {
		t.Fatalf("expected non-terminal output to preserve the full cell contents, got: %s", result)
	}
	if !strings.Contains(result, "tail") {
		t.Fatalf("expected non-terminal output to preserve later columns, got: %s", result)
	}
}

func TestOutput_EmptyOutput(t *testing.T) {
	buf := &bytes.Buffer{}
	out := NewOutput(FormatText, buf)

	err := out.Render()
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	result := buf.String()
	if result != "" {
		t.Errorf("Expected empty output, got: %s", result)
	}
}

func TestOutput_FormatDetection(t *testing.T) {
	tests := []struct {
		format   Format
		isJSON   bool
		expected string
	}{
		{FormatText, false, "text"},
		{FormatJSON, true, "json"},
	}

	for _, tt := range tests {
		buf := &bytes.Buffer{}
		out := NewOutput(tt.format, buf)

		if out.format != tt.format {
			t.Errorf("Format mismatch: got %v, want %v", out.format, tt.format)
		}
	}
}

func TestOutput_MultipleInfo_TextMode(t *testing.T) {
	buf := &bytes.Buffer{}
	out := NewOutput(FormatText, buf)

	out.Infof("Step 1")
	out.Infof("Step 2")
	out.Infof("Step 3")
	out.Success("All steps completed")
	err := out.Render()

	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	result := buf.String()
	lines := strings.Split(strings.TrimSpace(result), "\n")

	// Should have info lines + success line
	if len(lines) < 4 {
		t.Errorf("Expected at least 4 lines of output, got %d: %s", len(lines), result)
	}

	if !strings.Contains(result, "Step 1") {
		t.Errorf("Expected 'Step 1' in output")
	}
	if !strings.Contains(result, "Step 2") {
		t.Errorf("Expected 'Step 2' in output")
	}
	if !strings.Contains(result, "Step 3") {
		t.Errorf("Expected 'Step 3' in output")
	}
	if !strings.Contains(result, "✓ All steps completed") {
		t.Errorf("Expected success message in output")
	}
}

func TestOutput_Priority_ProtoMessage(t *testing.T) {
	buf := &bytes.Buffer{}
	out := NewOutput(FormatJSON, buf)

	// Set both protomessage and fields - protomessage should take priority
	msg, err := structpb.NewStruct(map[string]any{
		"name":  "proto",
		"count": 3,
	})
	if err != nil {
		t.Fatalf("build proto struct: %v", err)
	}
	out.SetProtoMessage(msg)
	out.AddField("ignoredField", "should not appear")

	err = out.Render()
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal JSON output: %v", err)
	}

	if decoded["name"] != "proto" || decoded["count"] != float64(3) {
		t.Fatalf("expected proto payload to win JSON precedence, got: %v", decoded)
	}
	if _, ok := decoded["ignoredField"]; ok {
		t.Errorf("expected proto payload to exclude field payload, got: %v", decoded)
	}
}

func TestNewOutput_NonTerminalWidthIsUnbounded(t *testing.T) {
	buf := &bytes.Buffer{}
	out := NewOutput(FormatText, buf)

	if out.termWidth != 0 {
		t.Errorf("expected non-terminal writer to keep width unbounded, got %d", out.termWidth)
	}
}
