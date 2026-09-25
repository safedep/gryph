package cli

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAdapters_InstallWritesOnlyDeclaredHookConfig installs each adapter
// into an empty home and checks that every file the install writes matches
// the adapter's HookConfigPaths. Self-protection guards only the declared
// paths, so a hook file outside them would be open to the governed agent.
func TestAdapters_InstallWritesOnlyDeclaredHookConfig(t *testing.T) {
	registry := agent.NewRegistry()
	registerAdapters(registry, nil, config.Default())

	for _, adapter := range registry.All() {
		t.Run(adapter.Name(), func(t *testing.T) {
			globs := adapter.HookConfigPaths()
			require.NotEmpty(t, globs)

			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("PATH", fakeAgentBinDir(t))
			require.NoError(t, os.MkdirAll(filepath.Join(home, "Applications", "Cursor.app"), 0o700))
			for _, g := range globs {
				require.NoError(t, os.MkdirAll(filepath.Join(home, configDirOf(g)), 0o700))
			}

			result, err := adapter.Install(context.Background(), agent.InstallOptions{})
			require.NoError(t, err)
			require.NotEmpty(t, result.HooksInstalled)

			written := filesUnder(t, home)
			require.NotEmpty(t, written)
			for _, rel := range written {
				assert.True(t, matchesAny(globs, rel), "%s wrote %s, which no HookConfigPaths glob covers", adapter.Name(), rel)
			}
		})
	}
}

// fakeAgentBinDir returns a directory with stand-in agent binaries, because
// some adapters detect their agent through a binary in PATH.
func fakeAgentBinDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"cursor"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\necho 0.0.0\n"), 0o700))
	}
	return dir
}

// configDirOf returns the directory part of a home glob before the first
// wildcard segment, without the "**/" anchor.
func configDirOf(glob string) string {
	rel := strings.TrimPrefix(glob, "**/")
	if i := strings.Index(rel, "/**"); i >= 0 {
		return rel[:i]
	}
	return filepath.Dir(rel)
}

func filesUnder(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	require.NoError(t, err)
	return out
}

func matchesAny(globs []string, rel string) bool {
	for _, g := range globs {
		if ok, _ := doublestar.Match(g, rel); ok {
			return true
		}
	}
	return false
}
