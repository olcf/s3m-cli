package util

import (
	"errors"
	"fmt"
	"os"
)

// FileDescriptor returns f.Fd() as an int when it fits in the target type.
func FileDescriptor(f *os.File) (int, error) {
	if f == nil {
		return 0, errors.New("nil file")
	}

	fd := f.Fd()

	const maxInt = int(^uint(0) >> 1)

	if fd > uintptr(maxInt) {
		return 0, fmt.Errorf("file descriptor %d exceeds int range", fd)
	}

	return int(fd), nil
}
