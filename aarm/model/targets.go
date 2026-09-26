package model

import (
	"cmp"
	"slices"
	"strings"

	"github.com/safedep/gryph/aarm/shellcmd"
)

// Hosts returns the lower-case hosts that the action contacts: the hosts of
// the shell command and the host of the request URL.
func (a *Action) Hosts() []string {
	var hosts []string
	if a.Shell != nil {
		hosts = append(hosts, a.Shell.Hosts...)
	}
	if h := urlHost(a.Parameters.URL); h != "" {
		hosts = append(hosts, h)
	}
	return compact(hosts)
}

// ReadPaths returns the paths that the action reads: the path of a file read
// and the read targets of a shell command.
func (a *Action) ReadPaths() []string {
	return a.readPaths(true)
}

// EntityPaths returns the paths that the session context records for the
// action: the action path, and the read and write targets of a shell
// command. It leaves out the guessed reads of a command that the walker does
// not know, such as the package patterns of "go test ./...". They are not
// files, and they would fill the capped entity set.
func (a *Action) EntityPaths() []string {
	paths := append([]string{a.Parameters.Path}, a.readPaths(false)...)
	return compact(append(paths, a.WritePaths()...))
}

func (a *Action) readPaths(guesses bool) []string {
	var paths []string
	if a.Type == ActionFileRead {
		paths = append(paths, a.Parameters.Path)
	}
	if a.Shell != nil {
		for _, t := range a.Shell.Targets {
			if t.Access == shellcmd.AccessRead && (guesses || !t.Guess) {
				paths = append(paths, cmp.Or(t.Glob, t.Path))
			}
		}
	}
	return compact(paths)
}

// WritePaths returns the paths that the action writes or removes: the path
// of a file write or delete and the write and remove targets of a shell
// command.
func (a *Action) WritePaths() []string {
	var paths []string
	if a.Type == ActionFileWrite || a.Type == ActionFileDelete {
		paths = append(paths, a.Parameters.Path)
	}
	if a.Shell != nil {
		for _, t := range a.Shell.Changes() {
			paths = append(paths, t.Path)
		}
	}
	return compact(paths)
}

// urlHost returns the host of a request URL. A URL that names no host that
// Gryph can read gives shellcmd.UnknownHost, so a rule on hosts fails
// closed.
func urlHost(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	if h := shellcmd.HostOf(raw); h != "" {
		return h
	}
	return shellcmd.UnknownHost
}

// compact drops empty values and duplicates, and keeps the order.
func compact(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}
