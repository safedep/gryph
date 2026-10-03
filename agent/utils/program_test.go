package utils

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionToken(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"2.1.15 (Claude Code)\n", "2.1.15"},
		{"codex-cli 0.5.0", "0.5.0"},
		{"  0.26.0  ", "0.26.0"},
		{"dev", "dev"},
		{"", ""},
		{"\n\t", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, VersionToken(tt.in), "%q", tt.in)
	}
}

// fakeProgram puts a program on PATH that prints a version and records
// each run in marker.
func fakeProgram(t *testing.T) (name, marker string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake program is a shell script")
	}
	dir := t.TempDir()
	marker = filepath.Join(dir, "ran")
	script := "#!/bin/sh\necho ran >> " + marker + "\necho 'fake-agent 3.2.1 (Fake)'\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fake-agent"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return "fake-agent", marker
}

func withPrivileged(t *testing.T, privileged bool) {
	t.Helper()
	restore := privilegedProcess
	privilegedProcess = func() bool { return privileged }
	t.Cleanup(func() { privilegedProcess = restore })
}

func TestProgramVersion_RunsAsUser(t *testing.T) {
	name, marker := fakeProgram(t)
	withPrivileged(t, false)

	assert.Equal(t, "3.2.1", ProgramVersion(context.Background(), name, "--version"))
	assert.FileExists(t, marker)
}

func TestProgramVersion_ChildHoldsThePipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake program is a shell script")
	}
	withPrivileged(t, false)
	dir := t.TempDir()
	// The program prints its version and exits, and a child of it keeps
	// the output pipe open far beyond the probe.
	script := "#!/bin/sh\necho '7.8.9'\n(sleep 30 &)\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fake-agent"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	start := time.Now()
	assert.Equal(t, "7.8.9", ProgramVersion(context.Background(), "fake-agent", "--version"))
	assert.Less(t, time.Since(start), programTimeout+pipeDelay+5*time.Second)
}

func TestProgramVersion_PrivilegedRunsNothing(t *testing.T) {
	name, marker := fakeProgram(t)
	withPrivileged(t, true)

	assert.Empty(t, ProgramVersion(context.Background(), name, "--version"))
	assert.NoFileExists(t, marker)
}

func TestProgramVersion_ContextForbidsExecution(t *testing.T) {
	name, marker := fakeProgram(t)
	withPrivileged(t, false)

	ctx := WithoutProgramExecution(context.Background())
	assert.Empty(t, ProgramVersion(ctx, name, "--version"))
	assert.NoFileExists(t, marker)
}

func TestProgramVersion_MissingProgram(t *testing.T) {
	withPrivileged(t, false)
	assert.Empty(t, ProgramVersion(context.Background(), "gryph-no-such-program-for-test", "--version"))
}
