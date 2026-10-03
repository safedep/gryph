package fanotify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/safedep/dry/log"
	"golang.org/x/sys/unix"
)

// permMask is the one permission event the watcher answers: the open.
// The kernel has no permission event for a rename or an unlink, so those
// are notices.
const permMask = unix.FAN_OPEN_PERM

// fileNoticeMask are the events of a marked file that the watcher records
// after the fact, and dirNoticeMask the events of the entries of a marked
// directory. Both need a group that reports file handles.
const (
	fileNoticeMask = unix.FAN_MODIFY | unix.FAN_ATTRIB | unix.FAN_CLOSE_WRITE | unix.FAN_DELETE_SELF | unix.FAN_MOVE_SELF
	dirNoticeMask  = unix.FAN_CREATE | unix.FAN_DELETE | unix.FAN_MOVED_FROM | unix.FAN_MOVED_TO | unix.FAN_MODIFY | unix.FAN_ATTRIB | unix.FAN_CLOSE_WRITE | unix.FAN_ONDIR | unix.FAN_EVENT_ON_CHILD
)

// infoHeader starts every record that follows the metadata of an event
// of a group that reports file handles.
type infoHeader struct {
	InfoType uint8
	Pad      uint8
	Len      uint16
}

// writeFlags are the open flags that ask for more than a read.
const writeFlags = unix.O_WRONLY | unix.O_RDWR | unix.O_TRUNC | unix.O_CREAT | unix.O_APPEND

type watcher struct {
	perm   int
	notice int
	// handles maps the file handle of every marked path to the path, for
	// the notice events, which name a file by its handle and not by a
	// descriptor.
	mu      sync.Mutex
	handles map[string]string
	files   []string
	dirs    []string
}

func available() error {
	fd, err := unix.FanotifyInit(unix.FAN_CLASS_CONTENT|unix.FAN_CLOEXEC, unix.O_RDONLY|unix.O_CLOEXEC)
	if err != nil {
		return fmt.Errorf("fanotify: open a permission group: %w", err)
	}
	return unix.Close(fd)
}

func open(opts Options) (*watcher, error) {
	// The event names the thread that opens, not its process: the flags
	// of the open are in the state of that thread, and the main thread of
	// a program with threads waits elsewhere.
	perm, err := unix.FanotifyInit(unix.FAN_CLASS_CONTENT|unix.FAN_CLOEXEC|unix.FAN_NONBLOCK|unix.FAN_REPORT_TID, unix.O_RDONLY|unix.O_LARGEFILE|unix.O_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("fanotify: open a permission group: %w", err)
	}
	notice, err := unix.FanotifyInit(unix.FAN_CLASS_NOTIF|unix.FAN_CLOEXEC|unix.FAN_NONBLOCK|unix.FAN_REPORT_DFID_NAME, unix.O_RDONLY|unix.O_LARGEFILE|unix.O_CLOEXEC)
	if err != nil {
		_ = unix.Close(perm)
		return nil, fmt.Errorf("fanotify: open a notification group: %w", err)
	}
	w := &watcher{perm: perm, notice: notice, handles: map[string]string{}}
	for _, f := range opts.Files {
		if err := w.markFile(f); err != nil {
			_ = w.close()
			return nil, err
		}
	}
	for _, d := range opts.Dirs {
		if err := w.markDir(d); err != nil {
			_ = w.close()
			return nil, err
		}
	}
	return w, nil
}

// markFile protects one file: the open asks, and a change is noticed.
func (w *watcher) markFile(path string) error {
	if err := w.remember(path); err != nil {
		return err
	}
	if err := unix.FanotifyMark(w.perm, unix.FAN_MARK_ADD, permMask, unix.AT_FDCWD, path); err != nil {
		return fmt.Errorf("fanotify: mark %s: %w", path, err)
	}
	if err := unix.FanotifyMark(w.notice, unix.FAN_MARK_ADD, fileNoticeMask, unix.AT_FDCWD, path); err != nil {
		return fmt.Errorf("fanotify: mark %s: %w", path, err)
	}
	w.mu.Lock()
	if !slices.Contains(w.files, path) {
		w.files = append(w.files, path)
	}
	w.mu.Unlock()
	return nil
}

func (w *watcher) remark(path string) error {
	return w.markFile(path)
}

// markDir protects the direct children of a directory: an open of a child
// asks, and a change of an entry is noticed with its name.
func (w *watcher) markDir(path string) error {
	if err := w.remember(path); err != nil {
		return err
	}
	if err := unix.FanotifyMark(w.perm, unix.FAN_MARK_ADD|unix.FAN_MARK_ONLYDIR, permMask|unix.FAN_EVENT_ON_CHILD, unix.AT_FDCWD, path); err != nil {
		return fmt.Errorf("fanotify: mark %s: %w", path, err)
	}
	if err := unix.FanotifyMark(w.notice, unix.FAN_MARK_ADD|unix.FAN_MARK_ONLYDIR, dirNoticeMask, unix.AT_FDCWD, path); err != nil {
		return fmt.Errorf("fanotify: mark %s: %w", path, err)
	}
	w.mu.Lock()
	w.dirs = append(w.dirs, path)
	w.mu.Unlock()
	return nil
}

// remember keeps the file handle of a path, so a notice that names the
// handle maps back to the path.
func (w *watcher) remember(path string) error {
	h, _, err := unix.NameToHandleAt(unix.AT_FDCWD, path, 0)
	if err != nil {
		return fmt.Errorf("fanotify: %s: %w", path, err)
	}
	w.mu.Lock()
	w.handles[handleKey(h.Type(), h.Bytes())] = path
	w.mu.Unlock()
	return nil
}

func handleKey(typ int32, bytes []byte) string {
	return strconv.Itoa(int(typ)) + ":" + string(bytes)
}

func (w *watcher) close() error {
	return errors.Join(unix.Close(w.perm), unix.Close(w.notice))
}

// serve reads both groups until ctx ends. The permission group comes
// first on every turn, because a process waits on each of its events.
func (w *watcher) serve(ctx context.Context, decide func(Request) bool, notice func(Change)) error {
	buf := make([]byte, 64*1024)
	for {
		if ctx.Err() != nil {
			return nil
		}
		busy := false
		if n, err := w.readGroup(w.perm, buf); err != nil {
			return err
		} else if n > 0 {
			busy = true
			w.answer(buf[:n], decide)
		}
		if n, err := w.readGroup(w.notice, buf); err != nil {
			return err
		} else if n > 0 {
			busy = true
			w.record(buf[:n], notice)
		}
		if !busy {
			w.wait(ctx)
		}
	}
}

// wait sleeps until a group is readable or ctx ends. The poll bounds the
// wait, so a cancel is seen within it.
func (w *watcher) wait(ctx context.Context) {
	fds := []unix.PollFd{{Fd: int32(w.perm), Events: unix.POLLIN}, {Fd: int32(w.notice), Events: unix.POLLIN}}
	deadline := time.Now().Add(500 * time.Millisecond)
	for ctx.Err() == nil && time.Now().Before(deadline) {
		n, err := unix.Poll(fds, 100)
		if err == nil && n > 0 {
			return
		}
		if err != nil && !errors.Is(err, unix.EINTR) {
			return
		}
	}
}

func (w *watcher) readGroup(fd int, buf []byte) (int, error) {
	n, err := unix.Read(fd, buf)
	switch {
	case errors.Is(err, unix.EAGAIN), errors.Is(err, unix.EINTR):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("fanotify: read: %w", err)
	}
	return n, nil
}

// answer decides every permission event in buf. A decision is written
// back as one response per event, and the file descriptor of the event is
// closed, else the opener waits forever.
func (w *watcher) answer(buf []byte, decide func(Request) bool) {
	for off := 0; off+int(unsafe.Sizeof(unix.FanotifyEventMetadata{})) <= len(buf); {
		m := (*unix.FanotifyEventMetadata)(unsafe.Pointer(&buf[off]))
		if m.Event_len == 0 || m.Vers != unix.FANOTIFY_METADATA_VERSION {
			return
		}
		if m.Mask&permMask != 0 && m.Fd >= 0 {
			req := Request{PID: int(m.Pid), Path: fdPath(int(m.Fd))}
			req.UID, req.Write, req.Unknown = openerOf(int(m.Pid))
			resp := uint32(unix.FAN_DENY)
			if decide(req) {
				resp = unix.FAN_ALLOW
			}
			r := unix.FanotifyResponse{Fd: m.Fd, Response: resp}
			if _, err := unix.Write(w.perm, (*[8]byte)(unsafe.Pointer(&r))[:]); err != nil {
				log.Warnf("fanotify: answer %s: %v", req.Path, err)
			}
		}
		if m.Fd >= 0 {
			_ = unix.Close(int(m.Fd))
		}
		off += int(m.Event_len)
	}
}

// record hands every notice event in buf to notice. The group reports
// file handles, so an event carries no descriptor: the records after the
// metadata name the marked file, or the marked directory and the entry.
func (w *watcher) record(buf []byte, notice func(Change)) {
	for off := 0; off+int(unsafe.Sizeof(unix.FanotifyEventMetadata{})) <= len(buf); {
		m := (*unix.FanotifyEventMetadata)(unsafe.Pointer(&buf[off]))
		if m.Event_len == 0 || m.Vers != unix.FANOTIFY_METADATA_VERSION {
			return
		}
		if m.Fd >= 0 {
			_ = unix.Close(int(m.Fd))
		}
		end := off + int(m.Event_len)
		if end > len(buf) {
			return
		}
		path := w.pathOf(buf[off+int(m.Metadata_len) : end])
		if notice != nil {
			if op := opOf(m.Mask); op != "" {
				uid, _ := realUID(int(m.Pid))
				notice(Change{Time: time.Now().UTC(), Op: op, Path: path, PID: int(m.Pid), UID: uid})
			}
		}
		off = end
	}
}

// pathOf reads the records of one event and returns the path they name:
// the marked file of a FID record, or the marked directory and the entry
// name of a DFID_NAME record. An unknown handle gives an empty path.
func (w *watcher) pathOf(records []byte) string {
	const headerLen = int(unsafe.Sizeof(infoHeader{}))
	const fsidLen = 8
	for off := 0; off+headerLen <= len(records); {
		h := (*infoHeader)(unsafe.Pointer(&records[off]))
		if h.Len == 0 || off+int(h.Len) > len(records) {
			return ""
		}
		rec := records[off : off+int(h.Len)]
		switch h.InfoType {
		case unix.FAN_EVENT_INFO_TYPE_FID, unix.FAN_EVENT_INFO_TYPE_DFID, unix.FAN_EVENT_INFO_TYPE_DFID_NAME:
			body := rec[headerLen+fsidLen:]
			if len(body) < 8 {
				return ""
			}
			handleBytes := int(*(*uint32)(unsafe.Pointer(&body[0])))
			handleType := *(*int32)(unsafe.Pointer(&body[4]))
			if 8+handleBytes > len(body) {
				return ""
			}
			w.mu.Lock()
			path := w.handles[handleKey(handleType, body[8:8+handleBytes])]
			w.mu.Unlock()
			if h.InfoType == unix.FAN_EVENT_INFO_TYPE_DFID_NAME && path != "" {
				name := body[8+handleBytes:]
				if i := bytesIndexZero(name); i >= 0 {
					name = name[:i]
				}
				if len(name) > 0 && string(name) != "." {
					path = filepath.Join(path, string(name))
				}
			}
			if path != "" {
				return path
			}
		}
		off += int(h.Len)
	}
	return ""
}

func bytesIndexZero(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return -1
}

func opOf(mask uint64) string {
	switch {
	case mask&(unix.FAN_DELETE_SELF|unix.FAN_DELETE) != 0:
		return "deleted"
	case mask&(unix.FAN_MOVE_SELF|unix.FAN_MOVED_FROM|unix.FAN_MOVED_TO) != 0:
		return "moved"
	case mask&unix.FAN_CREATE != 0:
		return "created"
	case mask&unix.FAN_ATTRIB != 0:
		return "attrib"
	case mask&(unix.FAN_MODIFY|unix.FAN_CLOSE_WRITE) != 0:
		return "modify"
	}
	return ""
}

// fdPath returns the path behind an event descriptor.
func fdPath(fd int) string {
	path, err := os.Readlink(filepath.Join("/proc/self/fd", strconv.Itoa(fd)))
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(path, " (deleted)")
}

// openerOf reads the real uid of the thread and the flags of the open it
// waits in. The thread is blocked in the open syscall while the event is
// open, so /proc/<tid>/syscall holds its arguments. A syscall that is not
// a plain open, or a thread that is gone, gives unknown.
func openerOf(tid int) (uid uint32, write bool, unknown bool) {
	uid, ok := realUID(tid)
	if !ok {
		return 0, false, true
	}
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(tid), "syscall"))
	if err != nil {
		return uid, false, true
	}
	fields := strings.Fields(string(data))
	if len(fields) < 4 {
		return uid, false, true
	}
	nr, err := strconv.Atoi(fields[0])
	if err != nil {
		return uid, false, true
	}
	// The kernel opens the file of an exec for a read, with no flags in
	// the arguments. The hook of a locked agent is such an open of the
	// managed binary, by the account of the user.
	if isExec(nr) {
		return uid, false, false
	}
	index := openFlagsIndex(nr)
	if index < 0 || index+1 >= len(fields) {
		return uid, false, true
	}
	flags, err := strconv.ParseUint(strings.TrimPrefix(fields[index+1], "0x"), 16, 64)
	if err != nil {
		return uid, false, true
	}
	return uid, flags&writeFlags != 0, false
}

func realUID(pid int) (uint32, bool) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == "Uid:" {
			uid, err := strconv.ParseUint(fields[1], 10, 32)
			if err != nil {
				return 0, false
			}
			return uint32(uid), true
		}
	}
	return 0, false
}
