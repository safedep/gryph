package projectdetection

import "errors"

// ErrNoProjectDetected is returned when no registered detector finds a project.
var ErrNoProjectDetected = errors.New("no project detected")

// ProjectInfo holds detected project metadata (extensible).
type ProjectInfo struct {
	Name string
}

// DetectProject detects project name from the given directory path using all
// registered detectors. Returns the first successful result, or
// (nil, ErrNoProjectDetected) when no detector finds a project.
func DetectProject(path string) (*ProjectInfo, error) {
	if path == "" {
		return nil, ErrNoProjectDetected
	}

	return defaultRegistry.Detect(path)
}
