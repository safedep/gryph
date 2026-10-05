//go:build windows

package nofollow

import "unsafe"

func unsafePointer[T any](v *T) unsafe.Pointer { return unsafe.Pointer(v) }
