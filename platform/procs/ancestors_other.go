//go:build !linux

package procs

import "time"

func ancestors(int) ([]Process, error) {
	return nil, ErrUnsupported
}

func startedAt(int) (time.Time, error) { return time.Time{}, ErrUnsupported }
