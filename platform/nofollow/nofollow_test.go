package nofollow

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("symlinks need a privilege on Windows")
		}
		require.NoError(t, err)
	}
}

func TestOpenDir_ReadWrite(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, "a", "b"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(base, "a", "b", "f.json"), []byte(`{"old":1}`), 0o600))

	d, err := OpenDir(base, filepath.Join("a", "b"))
	require.NoError(t, err)
	defer func() { require.NoError(t, d.Close()) }()
	assert.Equal(t, filepath.Join(base, "a", "b"), d.Path())

	data, err := d.ReadFile("f.json")
	require.NoError(t, err)
	assert.Equal(t, `{"old":1}`, string(data))

	require.NoError(t, d.WriteFile("f.json", []byte(`{"new":2}`), 0o600))
	data, err = os.ReadFile(filepath.Join(base, "a", "b", "f.json"))
	require.NoError(t, err)
	assert.Equal(t, `{"new":2}`, string(data))

	entries, err := os.ReadDir(filepath.Join(base, "a", "b"))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file stays behind")

	_, err = d.ReadFile("missing.json")
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = d.Stat("missing.json")
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestOpenDir_BaseItself(t *testing.T) {
	base := t.TempDir()
	d, err := OpenDir(base, ".")
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	require.NoError(t, d.WriteFile("f", []byte("x"), 0o600))
	require.NoError(t, d.Mkdir("sub", 0o700))
	require.NoError(t, d.Mkdir("sub", 0o700), "an existing directory is fine")
	info, err := os.Stat(filepath.Join(base, "sub"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestOpenDir_RejectsEscape(t *testing.T) {
	base := t.TempDir()
	for _, rel := range []string{"..", filepath.Join("..", "x"), base} {
		_, err := OpenDir(base, rel)
		assert.Error(t, err, rel)
	}
	d, err := OpenDir(base, ".")
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	for _, name := range []string{"", ".", "..", "a/b", `a\b`} {
		_, err := d.ReadFile(name)
		assert.Error(t, err, name)
	}
}

func TestOpenDir_RefusesLinkedComponent(t *testing.T) {
	base := t.TempDir()
	elsewhere := t.TempDir()
	symlinkOrSkip(t, elsewhere, filepath.Join(base, "cfg"))

	_, err := OpenDir(base, "cfg")
	assert.ErrorIs(t, err, ErrSymlink)

	require.NoError(t, os.Mkdir(filepath.Join(base, "real"), 0o700))
	symlinkOrSkip(t, elsewhere, filepath.Join(base, "real", "link"))
	_, err = OpenDir(base, filepath.Join("real", "link"))
	assert.ErrorIs(t, err, ErrSymlink)
}

func TestDir_RefusesLinkedFile(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(t.TempDir(), "target.json")
	require.NoError(t, os.WriteFile(target, []byte("keep"), 0o600))
	symlinkOrSkip(t, target, filepath.Join(base, "f.json"))

	d, err := OpenDir(base, ".")
	require.NoError(t, err)
	defer func() { _ = d.Close() }()

	_, err = d.ReadFile("f.json")
	assert.ErrorIs(t, err, ErrSymlink)

	err = d.WriteFile("f.json", []byte("new"), 0o600)
	assert.ErrorIs(t, err, ErrSymlink)
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "keep", string(data), "the target of the link is untouched")
	info, err := os.Lstat(filepath.Join(base, "f.json"))
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the link stays in place")

	err = d.Mkdir("f.json", 0o700)
	assert.Error(t, err)
}

func TestOpenDir_BaseMayBeLinked(t *testing.T) {
	real := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(real, "sub"), 0o700))
	linked := filepath.Join(t.TempDir(), "home")
	symlinkOrSkip(t, real, linked)

	d, err := OpenDir(linked, "sub")
	require.NoError(t, err, "the base is trusted")
	defer func() { _ = d.Close() }()
	require.NoError(t, d.WriteFile("f", []byte("x"), 0o600))
	_, err = os.Stat(filepath.Join(real, "sub", "f"))
	assert.NoError(t, err)
}

func TestDir_OpenDir(t *testing.T) {
	base := t.TempDir()
	d, err := OpenDir(base, ".")
	require.NoError(t, err)
	defer func() { _ = d.Close() }()

	require.NoError(t, d.Mkdir("users", 0o700))
	users, err := d.OpenDir("users")
	require.NoError(t, err)
	defer func() { _ = users.Close() }()
	assert.Equal(t, filepath.Join(base, "users"), users.Path())
	require.NoError(t, users.WriteFile("f", []byte("x"), 0o600))
	_, err = os.Stat(filepath.Join(base, "users", "f"))
	assert.NoError(t, err)

	_, err = d.OpenDir(filepath.Join("users", "f"))
	assert.Error(t, err, "one name only")
	_, err = users.OpenDir("f")
	assert.Error(t, err, "a file is not a directory")
	_, err = d.OpenDir("missing")
	assert.ErrorIs(t, err, os.ErrNotExist)

	elsewhere := t.TempDir()
	symlinkOrSkip(t, elsewhere, filepath.Join(base, "link"))
	_, err = d.OpenDir("link")
	assert.ErrorIs(t, err, ErrSymlink)
	assert.Error(t, d.Mkdir("link", 0o700), "a link in place of the directory is refused")
}

func TestDir_OpenReadDirRemove(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(base, "b.json"), []byte("b"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(base, "a.json"), []byte("a"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(base, "sub"), 0o700))
	d, err := OpenDir(base, ".")
	require.NoError(t, err)
	defer func() { _ = d.Close() }()

	info, err := d.Info()
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	entries, err := d.ReadDir()
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	assert.Equal(t, []string{"a.json", "b.json", "sub"}, names)

	f, err := d.Open("a.json")
	require.NoError(t, err)
	fi, err := f.Stat()
	require.NoError(t, err)
	assert.True(t, fi.Mode().IsRegular())
	if uid, links, ok := Owner(fi); ok {
		assert.Equal(t, uint32(os.Getuid()), uid)
		assert.Equal(t, uint64(1), links)
	}
	require.NoError(t, f.Close())

	_, err = d.Open("sub")
	assert.Error(t, err, "a directory is not a regular file")
	symlinkOrSkip(t, filepath.Join(base, "a.json"), filepath.Join(base, "link.json"))
	_, err = d.Open("link.json")
	assert.ErrorIs(t, err, ErrSymlink)

	require.NoError(t, d.Remove("link.json"))
	_, err = os.Lstat(filepath.Join(base, "link.json"))
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(base, "a.json"))
	assert.NoError(t, err, "the target of the link stays")
	require.NoError(t, d.Remove("a.json"))
	assert.ErrorIs(t, d.Remove("a.json"), os.ErrNotExist)
}
