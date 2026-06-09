package servercmd

import "testing"

func TestSanitizeLogValueEscapesLineBreaksAndControls(t *testing.T) {
	got := sanitizeLogValue("GET\r\n/path\tok")
	want := `GET\r\n/path?ok`

	if got != want {
		t.Fatalf("sanitizeLogValue() = %q, want %q", got, want)
	}
}

func TestTruncateForLogSanitizesPrefix(t *testing.T) {
	got := truncateForLog("abc\r\n12345")
	want := `abc\r\n123...`

	if got != want {
		t.Fatalf("truncateForLog() = %q, want %q", got, want)
	}
}
