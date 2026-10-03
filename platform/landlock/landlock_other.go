//go:build !linux

package landlock

func abi() (int, error) { return 0, ErrUnsupported }

func restrict(Options) error { return ErrUnsupported }

func ancestors(Options) []string { return nil }
