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

// Scope says who owns a policy source.
const (
	// ScopeUser is a source in the user's configuration directory.
	ScopeUser = "user"
	// ScopeManaged is a source in the system managed directory, owned by
	// the administrator.
	ScopeManaged = "managed"
	// ScopeBuiltin is a source compiled into Gryph.
	ScopeBuiltin = "builtin"
)

// Scoped is a source that names its owner. A source without it is a user
// source.
type Scoped interface {
	Scope() string
}

// ScopeOf returns the scope of a source.
func ScopeOf(s Source) string {
	if scoped, ok := s.(Scoped); ok {
		return scoped.Scope()
	}
	return ScopeUser
}

// FileSource loads a single policy file.
type FileSource struct {
	Path     string
	Optional bool
	// Trust, when set, verifies the path chain of the file before the load.
	// A file that fails it is an error, not an absent file, so a policy
	// that an administrator placed cannot be swapped for one the user owns.
	Trust func(path string) error
	scope string
}

func NewFileSource(path string) *FileSource {
	return &FileSource{Path: path}
}

func NewOptionalFileSource(path string) *FileSource {
	return &FileSource{Path: path, Optional: true}
}

// NewManagedFileSource returns an optional file source in the managed
// scope, whose file must pass trust.
func NewManagedFileSource(path string, trust func(path string) error) *FileSource {
	return &FileSource{Path: path, Optional: true, Trust: trust, scope: ScopeManaged}
}

func (s *FileSource) Name() string {
	if s.scope == ScopeManaged {
		return "managed-file:" + s.Path
	}
	return "file:" + s.Path
}

// Scope implements Scoped.
func (s *FileSource) Scope() string {
	if s.scope == "" {
		return ScopeUser
	}
	return s.scope
}

func (s *FileSource) Load(_ context.Context) ([]*pdp.Policy, error) {
	if err := trustFile(s.Path, s.Optional, s.Trust); err != nil {
		return nil, err
	}
	policy, err := pdp.LoadPolicyFile(s.Path)
	if err != nil {
		if s.Optional && errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return []*pdp.Policy{policy}, nil
}

// trustFile runs the trust check of a path. A missing optional file passes,
// because the load reports it as absent.
func trustFile(path string, optional bool, trust func(string) error) error {
	if trust == nil {
		return nil
	}
	if _, err := os.Lstat(path); err != nil && optional && errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err := trust(path); err != nil {
		return fmt.Errorf("managed policy %s is not trusted: %w", path, err)
	}
	return nil
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
	// Trust, when set, verifies the path chain of the directory and of each
	// file in it before the load.
	Trust func(path string) error
	scope string
}

func NewDirSource(path string) *DirSource {
	return &DirSource{Path: path}
}

func NewOptionalDirSource(path string) *DirSource {
	return &DirSource{Path: path, Optional: true}
}

// NewManagedDirSource returns an optional directory source in the managed
// scope, whose directory and files must pass trust.
func NewManagedDirSource(path string, trust func(path string) error) *DirSource {
	return &DirSource{Path: path, Optional: true, Trust: trust, scope: ScopeManaged}
}

func (s *DirSource) Name() string {
	if s.scope == ScopeManaged {
		return "managed-dir:" + s.Path
	}
	return "dir:" + s.Path
}

// Scope implements Scoped.
func (s *DirSource) Scope() string {
	if s.scope == "" {
		return ScopeUser
	}
	return s.scope
}

func (s *DirSource) Load(_ context.Context) ([]*pdp.Policy, error) {
	if err := trustFile(s.Path, s.Optional, s.Trust); err != nil {
		return nil, err
	}
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
		if err := trustFile(path, false, s.Trust); err != nil {
			return nil, err
		}
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
