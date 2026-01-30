package tui

import (
	"fmt"
	"io"
)

// tableWriter wraps an io.Writer and captures the first write error,
// skipping all subsequent writes after an error occurs. This is the
// same pattern used by bufio.Writer and encoding/csv.Writer.
type tableWriter struct {
	w   io.Writer
	err error
}

// printf writes a formatted string, doing nothing if a prior write failed.
func (tw *tableWriter) printf(format string, args ...any) {
	if tw.err != nil {
		return
	}
	_, tw.err = fmt.Fprintf(tw.w, format, args...)
}

// println writes arguments followed by a newline, doing nothing if a prior write failed.
func (tw *tableWriter) println(args ...any) {
	if tw.err != nil {
		return
	}
	_, tw.err = fmt.Fprintln(tw.w, args...)
}

// Err returns the first error encountered during any write, or nil.
func (tw *tableWriter) Err() error {
	return tw.err
}
