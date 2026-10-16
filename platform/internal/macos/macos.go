//go:build darwin

// Package macos calls the C interfaces of macOS that Gryph needs and that
// golang.org/x/sys does not cover: the launchd socket of a daemon and
// Authorization Services. It calls them the way x/sys calls libSystem,
// through the libc call path of the Go runtime, so the build needs no cgo
// and the release keeps its cross-build.
package macos

import (
	"syscall"
	"unsafe"

	// crypto/x509 links Security.framework for the external linker,
	// which a cgo build of the binary uses. This package calls into the
	// same framework and has no linker flag of its own.
	_ "crypto/x509"
)

// The runtime gives these to golang.org/x/sys under these names. Each one
// calls a C function by its address, on the system stack, as a system
// call. They set the errno result only when the function returns -1.
//
//go:linkname syscall_syscall syscall.syscall
//go:linkname syscall_syscall6 syscall.syscall6
func syscall_syscall(fn, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno)
func syscall_syscall6(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno)

//go:cgo_import_dynamic gryph_free free "/usr/lib/libSystem.B.dylib"

var freeAddr uintptr

// free releases memory that a C function allocated for the caller.
func free(p unsafe.Pointer) {
	_, _, _ = syscall_syscall(freeAddr, uintptr(p), 0, 0)
}
