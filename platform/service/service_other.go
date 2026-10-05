//go:build !linux

package service

import "context"

// The service manager of macOS (launchd) and Windows are later work. The
// hook client and the server do not depend on this package.
func install(context.Context, Spec) (*Result, error) { return nil, ErrUnsupported }

func remove(context.Context, string) (*Result, error) { return nil, ErrUnsupported }

func active(context.Context, string) bool { return false }
