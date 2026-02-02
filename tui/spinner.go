package tui

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type spinnerWriter struct {
	writer   io.Writer
	interval time.Duration
	err      error
}

func (sw *spinnerWriter) printf(format string, args ...any) {
	if sw.err != nil {
		return
	}

	_, sw.err = fmt.Fprintf(sw.writer, format, args...)
}

func (sw *spinnerWriter) isTerminal() bool {
	f, ok := sw.writer.(*os.File)
	if !ok {
		return false
	}

	return term.IsTerminal(int(f.Fd()))
}

type SpinnerOption func(*spinnerWriter)

func WithWriter(w io.Writer) SpinnerOption {
	return func(c *spinnerWriter) {
		c.writer = w
	}
}

func WithInterval(d time.Duration) SpinnerOption {
	return func(c *spinnerWriter) {
		c.interval = d
	}
}

func RunWithSpinner[T any](message string, fn func() (T, error), opts ...SpinnerOption) (T, error) {
	writer := spinnerWriter{
		writer:   os.Stderr,
		interval: 100 * time.Millisecond,
	}

	for _, opt := range opts {
		opt(&writer)
	}

	if !writer.isTerminal() {
		return fn()
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		i := 0
		for {
			select {
			case <-stop:
				writer.printf("\033[2K\r")
				return
			default:
				frame := spinnerFrames[i%len(spinnerFrames)]
				writer.printf("\033[2K\r%s%s%s %s", Cyan, frame, Reset, message)
				i++
				time.Sleep(writer.interval)
			}
		}
	}()

	result, err := fn()

	close(stop)
	wg.Wait()

	if err != nil {
		return result, err
	}

	return result, writer.err
}
