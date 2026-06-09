package util

import (
	"os"
	"testing"
)

func TestFileDescriptorReturnsInt(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "fd")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer f.Close()

	fd, err := FileDescriptor(f)
	if err != nil {
		t.Fatalf("FileDescriptor returned error: %v", err)
	}

	if uintptr(fd) != f.Fd() {
		t.Fatalf("FileDescriptor = %d, want %d", fd, f.Fd())
	}
}

func TestFileDescriptorNilFile(t *testing.T) {
	if _, err := FileDescriptor(nil); err == nil {
		t.Fatal("FileDescriptor(nil) error = nil, want non-nil")
	}
}
