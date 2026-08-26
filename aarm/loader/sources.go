package loader

import (
	"context"
	"errors"
	"io/fs"

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
