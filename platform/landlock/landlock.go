// Package landlock restricts the calling process and everything it starts
// with a Landlock ruleset: a set of paths that stay readable but take no
// write, while the rest of the file system stays as it is. Landlock is a
// Linux API (5.13 or later) that needs no privilege. A domain, once in
// place, cannot be lifted by the process or its children.
package landlock

import "errors"

// ErrUnsupported says that this platform or kernel has no Landlock.
var ErrUnsupported = errors.New("landlock: not supported on this platform")

// Options name what the ruleset protects.
type Options struct {
	// ReadOnly are the directories and files that stay readable and
	// executable but take no write, no create, no remove and no rename.
	// Each must exist.
	ReadOnly []string
}

// ABI returns the Landlock ABI version the kernel supports, or an error
// when the kernel has no Landlock or a seccomp filter refuses it.
func ABI() (int, error) {
	return abi()
}

// Restrict applies the ruleset to the calling process. Landlock allows
// what a rule names and nothing else, and a rule on a parent grants its
// rights to every child, so the ruleset grants every right to every entry
// of the file system except the read-only paths, which get the read and
// the execute rights only. The process keeps no right to create an entry
// directly inside an ancestor of a read-only path, because a rule on that
// ancestor would reach the read-only path too. Ancestors reports them.
//
// The kernel attaches the domain to the calling thread. Restrict locks
// the goroutine to its thread and keeps it locked, so the exec that
// follows runs on the restricted thread and the program inherits the
// domain.
func Restrict(opts Options) error {
	return restrict(opts)
}

// Ancestors returns the directories in which a process under the ruleset
// cannot create an entry: every ancestor of a read-only path.
func Ancestors(opts Options) []string {
	return ancestors(opts)
}
