// Package fanotify stops a write to a protected file by a process of a
// non-privileged account, with the permission events of the Linux
// fanotify API, and notices a change that it cannot stop. It needs
// CAP_SYS_ADMIN to open the group and CAP_SYS_PTRACE to read the open
// flags of the process that asks. Other platforms have no watcher.
package fanotify

import (
	"context"
	"errors"
	"time"
)

// ErrUnsupported says that this platform has no fanotify.
var ErrUnsupported = errors.New("fanotify: not supported on this platform")

// Options name what the watcher protects and who may write.
type Options struct {
	// Files are the files a non-exempt account cannot open for a write.
	Files []string
	// Dirs are the directories whose direct children get the same
	// protection, and whose entries the watcher notices.
	Dirs []string
	// ExemptUIDs may open anything. Root is always exempt.
	ExemptUIDs []uint32
}

// Request is one open of a protected path that waits for an answer.
type Request struct {
	PID  int
	UID  uint32
	Path string
	// Write is true when the open asks for a write, a truncate or a
	// create. Unknown is true when the watcher could not read the open
	// flags: a 32-bit process, an openat2 call, or a process that was
	// gone before the read.
	Write   bool
	Unknown bool
}

// Change is one change the watcher noticed and could not stop: a modify
// or an attribute change by an exempt account, or a rename or an unlink,
// which have no permission event.
type Change struct {
	Time time.Time
	// Op is modify, attrib, moved or deleted.
	Op   string
	Path string
	PID  int
	UID  uint32
}

// Watcher is one open fanotify group set over the protected paths.
type Watcher struct {
	w *watcher
}

// Available reports whether this process can open a permission group:
// Linux with CAP_SYS_ADMIN.
func Available() error {
	return available()
}

// Open marks the paths. A path that does not exist is an error, because
// a protection that covers nothing must not report a level.
func Open(opts Options) (*Watcher, error) {
	w, err := open(opts)
	if err != nil {
		return nil, err
	}
	return &Watcher{w: w}, nil
}

// Serve answers every permission request with decide, and hands every
// change to notice, until ctx ends. A nil notice ignores the changes.
func (w *Watcher) Serve(ctx context.Context, decide func(Request) bool, notice func(Change)) error {
	return w.w.serve(ctx, decide, notice)
}

// Remark marks a file again. A rename or an unlink leaves the mark on the
// old inode, so the path, when a file exists at it again, needs a new
// mark.
func (w *Watcher) Remark(path string) error {
	return w.w.remark(path)
}

// Close releases the groups. A watcher that is closed stops nothing.
func (w *Watcher) Close() error {
	return w.w.close()
}

// Decide is the default answer: an exempt account may do anything, and
// another account may open for a read only. An open whose flags cannot
// be read counts as a write, so the protection fails closed.
func Decide(exempt []uint32, r Request) bool {
	if r.UID == 0 {
		return true
	}
	for _, uid := range exempt {
		if r.UID == uid {
			return true
		}
	}
	return !r.Write && !r.Unknown
}
