// Package main provides the entry point for gryph.
package main

import (
	"fmt"
	"os"

	"github.com/safedep/gryph/cli"
)

// ExitCoder is an interface for errors that carry a custom exit code and message.
type ExitCoder interface {
	ExitCode() int
	Message() string
}

func main() {
	if err := cli.Execute(); err != nil {
		// Check if the error specifies a custom exit code
		if exitErr, ok := err.(ExitCoder); ok {
			fmt.Fprint(os.Stderr, exitErr.Message())
			os.Exit(exitErr.ExitCode())
		}

		// Default error handling
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
