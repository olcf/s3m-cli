package output

import (
	"testing"
	"time"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{"zero", 0, "0 B"},
		{"bytes", 100, "100 B"},
		{"kilobytes", 1024, "1.0 KiB"},
		{"megabytes", 1024 * 1024, "1.0 MiB"},
		{"gigabytes", 1024 * 1024 * 1024, "1.0 GiB"},
		{"mixed", 1536, "1.5 KiB"},
		{"large", 1024*1024*1024 + 512*1024*1024, "1.5 GiB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatBytes(tt.bytes)
			if result != tt.expected {
				t.Errorf("FormatBytes(%d) = %s, want %s", tt.bytes, result, tt.expected)
			}
		})
	}
}

func TestShortID(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		expected string
	}{
		{"short", "abc", "abc"},
		{"exact", "abcdefgh", "abcdefgh"},
		{"long", "abcdefghijklmnop", "abcdefgh"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShortID(tt.id)
			if result != tt.expected {
				t.Errorf("ShortID(%s) = %s, want %s", tt.id, result, tt.expected)
			}
		})
	}
}

func TestFormatTimestamp(t *testing.T) {
	testTime := time.Date(2026, 1, 8, 14, 30, 0, 0, time.UTC)
	result := FormatTimestamp(testTime)
	expected := "2026-01-08 14:30"

	if result != expected {
		t.Errorf("FormatTimestamp() = %s, want %s", result, expected)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"seconds", 45 * time.Second, "45s"},
		{"minutes", 5 * time.Minute, "5m"},
		{"hours", 3 * time.Hour, "3h"},
		{"days", 48 * time.Hour, "2d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDuration(tt.duration)
			if result != tt.expected {
				t.Errorf("FormatDuration(%v) = %s, want %s", tt.duration, result, tt.expected)
			}
		})
	}
}
