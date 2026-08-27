package loader

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/safedep/gryph/aarm/pdp"
)

// FileSource loads a single policy file.
type FileSource struct {
	Path     string
	Optional bool
}

func NewFileSource(path string) *FileSource {
	return &FileSource{Path: path}
}

func NewOptionalFileSource(path string) *FileSource {
	return &FileSource{Path: path, Optional: true}
}

func (s *FileSource) Name() string {
	return "file:" + s.Path
}

func (s *FileSource) Load(_ context.Context) ([]*pdp.Policy, error) {
	policy, err := pdp.LoadPolicyFile(s.Path)
	if err != nil {
		if s.Optional && errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return []*pdp.Policy{policy}, nil
}

// StaticSource yields policy documents the caller already parsed. It lets a
// caller include in-memory content in a merge, without a re-read from disk.
type StaticSource struct {
	SourceName string
	Docs       []*pdp.Policy
}

func NewStaticSource(name string, docs ...*pdp.Policy) *StaticSource {
	return &StaticSource{SourceName: name, Docs: docs}
}

func (s *StaticSource) Name() string { return s.SourceName }

func (s *StaticSource) Load(_ context.Context) ([]*pdp.Policy, error) {
	return s.Docs, nil
}

// DirSource loads every YAML policy file in one directory as a separate policy
// document. It reads a single fixed, operator-owned directory. It is not a
// configurable or repo-reaching source. Files load in sorted name order so the
// merge is deterministic. A malformed file returns an error that names the
// file.
type DirSource struct {
	Path     string
	Optional bool
}

func NewDirSource(path string) *DirSource {
	return &DirSource{Path: path}
}

func NewOptionalDirSource(path string) *DirSource {
	return &DirSource{Path: path, Optional: true}
}

func (s *DirSource) Name() string {
	return "dir:" + s.Path
}

func (s *DirSource) Load(_ context.Context) ([]*pdp.Policy, error) {
	entries, err := os.ReadDir(s.Path)
	if err != nil {
		if s.Optional && errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	names := policyFileNames(entries)
	docs := make([]*pdp.Policy, 0, len(names))
	for _, name := range names {
		path := filepath.Join(s.Path, name)
		policy, err := pdp.LoadPolicyFile(path)
		if err != nil {
			return nil, fmt.Errorf("policy file %s: %w", path, err)
		}
		docs = append(docs, policy)
	}
	return docs, nil
}

// PolicyFilesInDir returns the sorted YAML policy file paths in dir. A missing
// directory returns no files and no error, so callers treat an absent policies
// directory as empty. Callers that need per-file inspection (list, install)
// share this scan with DirSource.
func PolicyFilesInDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	names := policyFileNames(entries)
	files := make([]string, len(names))
	for i, name := range names {
		files[i] = filepath.Join(dir, name)
	}
	return files, nil
}

func policyFileNames(entries []os.DirEntry) []string {
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if IsPolicyFileName(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// IsPolicyFileName reports whether name has a policy-file extension.
func IsPolicyFileName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".yaml", ".yml":
		return true
	default:
		return false
	}
}

// NormalizePolicyFileName appends the default .yaml extension when name has no
// policy-file extension, so "x" and "x.yaml" resolve to the same file.
func NormalizePolicyFileName(name string) string {
	if IsPolicyFileName(name) {
		return name
	}
	return name + ".yaml"
}
