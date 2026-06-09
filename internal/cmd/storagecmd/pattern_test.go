package storagecmd

import (
	"testing"
)

func TestIsGlobPattern(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"file.txt", false},
		{"data/file.txt", false},
		{"data/", false},
		{"*.txt", true},
		{"data/*.txt", true},
		{"data/**", true},
		{"file?.txt", true},
		{"[abc].txt", true},
	}

	for _, tt := range tests {
		if got := isGlobPattern(tt.pattern); got != tt.want {
			t.Errorf("isGlobPattern(%q) = %v, want %v", tt.pattern, got, tt.want)
		}
	}
}
