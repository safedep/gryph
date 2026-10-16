//go:build !linux && !darwin && !windows

package account

import "context"

func createSystem(context.Context, string, string) error {
	return ErrUnsupported
}
