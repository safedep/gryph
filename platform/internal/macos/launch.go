//go:build darwin

package macos

import (
	"fmt"
	"syscall"
	"unsafe"
)

//go:cgo_import_dynamic gryph_launch_activate_socket launch_activate_socket "/usr/lib/libSystem.B.dylib"

var launchActivateSocketAddr uintptr

// ActivateSocket returns the descriptors of the socket that launchd holds
// for this job under name, the key in the Sockets dictionary of its job
// file. A process that launchd did not start gets ESRCH, and a name the
// job does not have gets ENOENT. A second call for one name gets EALREADY.
func ActivateSocket(name string) ([]int, error) {
	cname, err := syscall.BytePtrFromString(name)
	if err != nil {
		return nil, err
	}
	var fds *int32
	var count uintptr
	r, _, _ := syscall_syscall(launchActivateSocketAddr,
		uintptr(unsafe.Pointer(cname)), uintptr(unsafe.Pointer(&fds)), uintptr(unsafe.Pointer(&count)))
	if errno := int32(r); errno != 0 {
		return nil, fmt.Errorf("launch_activate_socket %s: %w", name, syscall.Errno(errno))
	}
	if fds == nil {
		return nil, nil
	}
	defer free(unsafe.Pointer(fds))
	out := make([]int, 0, count)
	for _, fd := range unsafe.Slice(fds, count) {
		out = append(out, int(fd))
	}
	return out, nil
}
