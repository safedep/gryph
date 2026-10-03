//go:build !windows

package config

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEntry describes one path in a fake file system for the chain check.
type fakeEntry struct {
	mode   os.FileMode
	uid    uint32
	target string
}

type fakeInfo struct {
	name string
	e    fakeEntry
}

func (f fakeInfo) Name() string       { return f.name }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() os.FileMode  { return f.e.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.e.mode.IsDir() }
func (f fakeInfo) Sys() any           { return &syscall.Stat_t{Uid: f.e.uid} }

// withFakeChain replaces the file system calls of the chain check with a
// map from path to entry.
func withFakeChain(t *testing.T, fs map[string]fakeEntry) {
	t.Helper()
	restoreLstat, restoreReadlink := lstatManaged, readlinkManaged
	lstatManaged = func(path string) (os.FileInfo, error) {
		e, ok := fs[path]
		if !ok {
			return nil, &os.PathError{Op: "lstat", Path: path, Err: os.ErrNotExist}
		}
		return fakeInfo{name: filepath.Base(path), e: e}, nil
	}
	readlinkManaged = func(path string) (string, error) {
		e, ok := fs[path]
		if !ok || e.mode&os.ModeSymlink == 0 {
			return "", &os.PathError{Op: "readlink", Path: path, Err: os.ErrInvalid}
		}
		return e.target, nil
	}
	t.Cleanup(func() {
		lstatManaged, readlinkManaged = restoreLstat, restoreReadlink
	})
}

const rootDir = os.ModeDir | 0o755

func trustedChain() map[string]fakeEntry {
	return map[string]fakeEntry{
		"/":                             {mode: rootDir},
		"/etc":                          {mode: rootDir},
		"/etc/safedep":                  {mode: rootDir},
		"/etc/safedep/gryph":            {mode: rootDir},
		"/etc/safedep/gryph/config.yml": {mode: 0o644},
	}
}

func TestVerifyManagedPathTrust(t *testing.T) {
	const path = "/etc/safedep/gryph/config.yml"

	tests := []struct {
		name    string
		change  func(fs map[string]fakeEntry)
		wantErr string
	}{
		{name: "root owned chain", change: func(map[string]fakeEntry) {}},
		{
			name:    "group writable directory",
			change:  func(fs map[string]fakeEntry) { fs["/etc/safedep"] = fakeEntry{mode: os.ModeDir | 0o775} },
			wantErr: "/etc/safedep is writable by group or other",
		},
		{
			name:    "other writable file",
			change:  func(fs map[string]fakeEntry) { fs[path] = fakeEntry{mode: 0o666} },
			wantErr: path + " is writable by group or other",
		},
		{
			name: "sticky world writable directory",
			change: func(fs map[string]fakeEntry) {
				fs["/etc/safedep"] = fakeEntry{mode: os.ModeDir | os.ModeSticky | 0o777}
			},
		},
		{
			name:    "directory owned by a user",
			change:  func(fs map[string]fakeEntry) { fs["/etc/safedep/gryph"] = fakeEntry{mode: rootDir, uid: 1000} },
			wantErr: "/etc/safedep/gryph is not owned by root",
		},
		{
			name:    "file owned by a user",
			change:  func(fs map[string]fakeEntry) { fs[path] = fakeEntry{mode: 0o644, uid: 1000} },
			wantErr: path + " is not owned by root",
		},
		{
			name: "root owned symbolic link to a trusted target",
			change: func(fs map[string]fakeEntry) {
				fs["/etc"] = fakeEntry{mode: os.ModeSymlink | 0o777, target: "private/etc"}
				fs["/private"] = fakeEntry{mode: rootDir}
				fs["/private/etc"] = fakeEntry{mode: rootDir}
			},
		},
		{
			name: "symbolic link owned by a user",
			change: func(fs map[string]fakeEntry) {
				fs["/etc/safedep"] = fakeEntry{mode: os.ModeSymlink | 0o777, uid: 1000, target: "/opt/safedep"}
				fs["/opt"] = fakeEntry{mode: rootDir}
				fs["/opt/safedep"] = fakeEntry{mode: rootDir}
			},
			wantErr: "/etc/safedep is not owned by root",
		},
		{
			name: "symbolic link to a writable target",
			change: func(fs map[string]fakeEntry) {
				fs["/etc/safedep"] = fakeEntry{mode: os.ModeSymlink | 0o777, target: "/opt/safedep"}
				fs["/opt"] = fakeEntry{mode: rootDir}
				fs["/opt/safedep"] = fakeEntry{mode: os.ModeDir | 0o777}
			},
			wantErr: "/opt/safedep is writable by group or other",
		},
		{
			name: "symbolic link loop",
			change: func(fs map[string]fakeEntry) {
				fs["/etc/safedep"] = fakeEntry{mode: os.ModeSymlink | 0o777, target: "/etc/safedep"}
			},
			wantErr: "too many symbolic links",
		},
		{
			name:    "missing component",
			change:  func(fs map[string]fakeEntry) { delete(fs, "/etc/safedep/gryph") },
			wantErr: "file does not exist",
		},
		{
			name:    "socket in place of the file",
			change:  func(fs map[string]fakeEntry) { fs[path] = fakeEntry{mode: os.ModeSocket | 0o600} },
			wantErr: "is not a directory or a regular file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := trustedChain()
			tt.change(fs)
			withFakeChain(t, fs)

			err := verifyManagedPathTrust(path)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestVerifyManagedPathTrust_RelativePath(t *testing.T) {
	withFakeChain(t, trustedChain())
	assert.ErrorContains(t, verifyManagedPathTrust("etc/safedep/gryph/config.yml"), "not an absolute path")
}

// TestVerifyManagedPathTrust_RealFiles runs the check against the real file
// system. Only root can create a trusted chain, so a non-root run checks
// only that its own files are refused.
func TestVerifyManagedPathTrust_RealFiles(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "safedep", "gryph")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, configFileName)
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))

	if os.Geteuid() != 0 {
		assert.ErrorContains(t, verifyManagedPathTrust(path), "is not owned by root")
		return
	}

	// The temporary directory lives under a sticky, world writable parent,
	// which the check allows.
	require.NoError(t, verifyManagedPathTrust(path))

	require.NoError(t, os.Chmod(filepath.Join(base, "safedep"), 0o775))
	assert.ErrorContains(t, verifyManagedPathTrust(path), "safedep is writable by group or other")
	require.NoError(t, os.Chmod(filepath.Join(base, "safedep"), 0o755))

	require.NoError(t, os.Chown(dir, 65534, 65534))
	assert.ErrorContains(t, verifyManagedPathTrust(path), "gryph is not owned by root")
	require.NoError(t, os.Chown(dir, 0, 0))

	link := filepath.Join(base, "link")
	require.NoError(t, os.Symlink(filepath.Join(base, "safedep"), link))
	assert.NoError(t, verifyManagedPathTrust(filepath.Join(link, "gryph", configFileName)))
}
