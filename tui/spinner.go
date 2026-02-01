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

type spinnerConfig struct {
	writer   io.Writer
	interval time.Duration
}

type SpinnerOption func(*spinnerConfig)

func WithWriter(w io.Writer) SpinnerOption {
	return func(c *spinnerConfig) {
		c.writer = w
	}
}

func WithInterval(d time.Duration) SpinnerOption {
	return func(c *spinnerConfig) {
		c.interval = d
	}
}

func isWriterTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func RunWithSpinner[T any](message string, fn func() (T, error), opts ...SpinnerOption) (T, error) {
	cfg := spinnerConfig{
		writer:   os.Stderr,
		interval: 100 * time.Millisecond,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	if !isWriterTerminal(cfg.writer) {
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
				fmt.Fprintf(cfg.writer, "\033[2K\r")
				return
			default:
				frame := spinnerFrames[i%len(spinnerFrames)]
				fmt.Fprintf(cfg.writer, "\033[2K\r%s%s%s %s", Cyan, frame, Reset, message)
				i++
				time.Sleep(cfg.interval)
			}
		}
	}()

	result, err := fn()
	close(stop)
	wg.Wait()

	return result, err
}
