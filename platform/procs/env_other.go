//go:build !linux

package procs

func hasEnv(int, string) (bool, error) { return false, ErrUnsupported }
