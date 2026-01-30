// Package main provides the entry point for gryph.
package main

import (
	"fmt"
	"os"

	"github.com/safedep/gryph/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		if exitErr, ok := err.(cli.ExitCoder); ok {
			fmt.Fprint(os.Stderr, exitErr.Message())
			os.Exit(exitErr.ExitCode())
		}

		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
