package tui

import (
	"fmt"
	"io"
)

const (
	clearLine       = "\033[2K"
	carriageReturn  = "\r"
	clearLineReturn = clearLine + carriageReturn
)

// ProgressWriter writes progress updates to a single terminal line.
type ProgressWriter struct {
	w          io.Writer
	color      *Colorizer
	isTerminal bool
}

// NewProgressWriter creates a new ProgressWriter.
func NewProgressWriter(w io.Writer, useColors bool) *ProgressWriter {
	return &ProgressWriter{
		w:          w,
		color:      NewColorizer(useColors),
		isTerminal: IsTerminal(),
	}
}

// Update clears the line and writes new progress text.
func (p *ProgressWriter) Update(format string, args ...any) {
	if !p.isTerminal {
		return
	}
	msg := fmt.Sprintf(format, args...)
	fmt.Fprint(p.w, clearLineReturn+p.color.Dim(msg))
}

// Clear clears the progress line.
func (p *ProgressWriter) Clear() {
	if !p.isTerminal {
		return
	}
	fmt.Fprint(p.w, clearLineReturn)
}
