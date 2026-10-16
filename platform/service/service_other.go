//go:build !linux && !darwin

package service

import "context"

// The service manager of Windows is later work. The
// hook client and the server do not depend on this package.
func install(context.Context, Spec) (*Result, error) { return nil, ErrUnsupported }

func remove(context.Context, string) (*Result, error) { return nil, ErrUnsupported }

func active(context.Context, string) bool { return false }

func reloadHint(string) string { return "" }

func logHint(string) string { return "" }
