//go:build !linux && !darwin && !windows

package schedule

import (
	"context"
	"fmt"
	"runtime"
)

func install(context.Context, Job) (*Result, error) {
	return nil, fmt.Errorf("schedule: no scheduler support on %s", runtime.GOOS)
}

func remove(context.Context, string) (*Result, error) {
	return nil, fmt.Errorf("schedule: no scheduler support on %s", runtime.GOOS)
}

func installSystemWide(context.Context, Job) (*Result, error) {
	return nil, fmt.Errorf("schedule: no scheduler support on %s", runtime.GOOS)
}

func removeSystemWide(context.Context, string) (*Result, error) {
	return nil, fmt.Errorf("schedule: no scheduler support on %s", runtime.GOOS)
}
