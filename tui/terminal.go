package tui

import (
	"io"
	"os"

	"golang.org/x/term"
)

const (
	// DefaultTerminalWidth is used when terminal width cannot be detected.
	DefaultTerminalWidth = 80
	// MinTerminalWidth is the minimum width we'll use for rendering.
	MinTerminalWidth = 60
	// MaxTerminalWidth is the maximum width we'll use for rendering.
	MaxTerminalWidth = 200
)

// GetTerminalWidth returns the current terminal width.
// Falls back to DefaultTerminalWidth if detection fails or output is not a TTY.
func GetTerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return DefaultTerminalWidth
	}
	if width < MinTerminalWidth {
		return MinTerminalWidth
	}
	if width > MaxTerminalWidth {
		return MaxTerminalWidth
	}
	return width
}

// IsTerminal returns true if stdout is a terminal.
func IsTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// IsWriterTerminal returns true if w is backed by a terminal file descriptor.
func IsWriterTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}
