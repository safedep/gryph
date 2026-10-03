// Package kernel reads the settings of the running kernel that decide how
// well Gryph resists a same-user adversary, such as the Yama ptrace scope
// and the user namespace limits. Only Linux exposes them. On every other
// platform the read returns ErrUnsupported.
package kernel

import "errors"

// ErrUnsupported is the error of a platform that has no such setting.
var ErrUnsupported = errors.New("kernel: no such setting on this platform")

// Setting returns the value of a kernel setting by its sysctl name, for
// example kernel.yama.ptrace_scope, with surrounding space removed. A
// setting that the kernel does not have returns fs.ErrNotExist.
func Setting(name string) (string, error) {
	return setting(name)
}
