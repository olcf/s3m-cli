package output

import (
	"io"
	"os"

	"github.com/olcf/s3m-cli/internal/util"

	"golang.org/x/term"
)

// Terminal dimension constants.
const (
	DefaultTerminalWidth  = 120
	DefaultTerminalHeight = 40
)

// TerminalDimensions returns terminal width and height.
// Returns (0, 0) if not a terminal or detection fails.
func TerminalDimensions(w io.Writer) (width, height int) {
	if f, ok := w.(*os.File); ok {
		fd, err := util.FileDescriptor(f)
		if err != nil {
			return 0, 0
		}

		if width, height, err := term.GetSize(fd); err == nil {
			return width, height
		}
	}

	return 0, 0
}

// IsTerminal checks if writer is a terminal.
func IsTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		fd, err := util.FileDescriptor(f)
		if err != nil {
			return false
		}

		return term.IsTerminal(fd)
	}

	return false
}
