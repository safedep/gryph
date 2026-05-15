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

// DirSource loads every non-hidden *.yaml / *.yml file in a directory,
// non-recursively, sorted by filename.
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

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !hasYAMLExt(name) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	docs := make([]*pdp.Policy, 0, len(names))
	for _, name := range names {
		policy, err := pdp.LoadPolicyFile(filepath.Join(s.Path, name))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		docs = append(docs, policy)
	}
	return docs, nil
}

// ConventionalSource walks upward from StartDir looking for any file in
// Filenames. The first match wins. When nothing is found it returns zero documents.
type ConventionalSource struct {
	StartDir  string
	Filenames []string
	StopAt    string
}

var DefaultConventionalFilenames = []string{".gryph-policy.yml", ".gryph-policy.yaml"}

func NewConventionalSource(startDir string) *ConventionalSource {
	return &ConventionalSource{
		StartDir:  startDir,
		Filenames: append([]string(nil), DefaultConventionalFilenames...),
	}
}

func (s *ConventionalSource) Name() string {
	return "conventional:" + s.StartDir
}

func (s *ConventionalSource) Load(_ context.Context) ([]*pdp.Policy, error) {
	if s.StartDir == "" {
		return nil, nil
	}
	filenames := s.Filenames
	if len(filenames) == 0 {
		filenames = DefaultConventionalFilenames
	}

	dir, err := filepath.Abs(s.StartDir)
	if err != nil {
		return nil, fmt.Errorf("resolve start dir: %w", err)
	}
	stopAt := s.StopAt
	if stopAt != "" {
		if abs, err := filepath.Abs(stopAt); err == nil {
			stopAt = abs
		}
	}

	for {
		for _, name := range filenames {
			candidate := filepath.Join(dir, name)
			info, err := os.Stat(candidate)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				return nil, err
			}
			if info.IsDir() {
				continue
			}
			policy, err := pdp.LoadPolicyFile(candidate)
			if err != nil {
				return nil, err
			}
			return []*pdp.Policy{policy}, nil
		}

		if stopAt != "" && dir == stopAt {
			return nil, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, nil
		}
		dir = parent
	}
}

func hasYAMLExt(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml"
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
