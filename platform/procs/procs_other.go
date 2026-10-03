//go:build !linux && !darwin && !windows

package procs

import (
	"fmt"
	"runtime"
)

func list() ([]Process, error) {
	return nil, fmt.Errorf("list processes: no support on %s", runtime.GOOS)
}
