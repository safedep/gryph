//go:build !windows

package nofollow

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// dirHandle is an open directory file descriptor. Every operation is
// relative to it, so the path cannot change under the caller.
type dirHandle struct {
	fd   int
	path string
}

const dirOpenFlags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC

func openDir(base string, comps []string) (dirHandle, error) {
	fd, err := unix.Open(base, dirOpenFlags&^unix.O_NOFOLLOW, 0)
	if err != nil {
		return dirHandle{}, fmt.Errorf("open %s: %w", base, err)
	}
	h := dirHandle{fd: fd, path: base}
	if len(comps) == 0 {
		return h, nil
	}
	next, err := h.openBeneath(comps)
	closeErr := h.close()
	if err != nil {
		return dirHandle{}, err
	}
	if closeErr != nil {
		_ = next.close()
		return dirHandle{}, closeErr
	}
	return next, nil
}

// walk opens each component relative to the previous one. The directory
// that the walk ends in is returned.
func (h dirHandle) walk(comps []string) (dirHandle, error) {
	cur := h
	for i, comp := range comps {
		fd, err := unix.Openat(cur.fd, comp, dirOpenFlags, 0)
		if i > 0 {
			_ = cur.close()
		}
		if err != nil {
			return dirHandle{}, wrapLink(filepath.Join(append([]string{h.path}, comps[:i+1]...)...), err)
		}
		cur = dirHandle{fd: fd, path: filepath.Join(cur.path, comp)}
	}
	return cur, nil
}

func (h dirHandle) close() error {
	if h.fd < 0 {
		return nil
	}
	return unix.Close(h.fd)
}

func (h dirHandle) stat(name string) (fs.FileInfo, error) {
	var st unix.Stat_t
	if err := unix.Fstatat(h.fd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nil, &os.PathError{Op: "lstat", Path: filepath.Join(h.path, name), Err: err}
	}
	return os.Lstat(filepath.Join(h.path, name)) // mode and size from the kernel, through the standard type
}

func (h dirHandle) readFile(name string) ([]byte, error) {
	fd, err := unix.Openat(h.fd, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, wrapLink(filepath.Join(h.path, name), err)
	}
	f := os.NewFile(uintptr(fd), filepath.Join(h.path, name))
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", f.Name())
	}
	return io.ReadAll(f)
}

func (h dirHandle) writeFile(name string, data []byte, perm os.FileMode) error {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return err
	}
	tmp := tempName(name, suffix)
	fd, err := unix.Openat(h.fd, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(perm.Perm()))
	if err != nil {
		return &os.PathError{Op: "create", Path: filepath.Join(h.path, tmp), Err: err}
	}
	f := os.NewFile(uintptr(fd), filepath.Join(h.path, tmp))
	if _, err := f.Write(data); err != nil {
		return h.discard(f, tmp, err)
	}
	if err := f.Sync(); err != nil {
		return h.discard(f, tmp, err)
	}
	if err := f.Close(); err != nil {
		return h.discard(nil, tmp, err)
	}
	if err := unix.Renameat(h.fd, tmp, h.fd, name); err != nil {
		return h.discard(nil, tmp, &os.LinkError{Op: "rename", Old: filepath.Join(h.path, tmp), New: filepath.Join(h.path, name), Err: err})
	}
	return unix.Fsync(h.fd)
}

func (h dirHandle) discard(f *os.File, tmp string, err error) error {
	if f != nil {
		_ = f.Close()
	}
	_ = unix.Unlinkat(h.fd, tmp, 0)
	return err
}

func (h dirHandle) mkdir(name string, perm os.FileMode) error {
	if err := unix.Mkdirat(h.fd, name, uint32(perm.Perm())); err != nil {
		return &os.PathError{Op: "mkdir", Path: filepath.Join(h.path, name), Err: err}
	}
	return nil
}

// wrapLink turns the errno of a refused link into ErrSymlink, so callers
// can name the cause.
func wrapLink(path string, err error) error {
	if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
		return fmt.Errorf("%s: %w", path, ErrSymlink)
	}
	return &os.PathError{Op: "open", Path: path, Err: err}
}
