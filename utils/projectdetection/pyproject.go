package projectdetection

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const pyprojectToml = "pyproject.toml"

var pyprojectNameRe = regexp.MustCompile(`^\s*name\s*=\s*["']([^"']+)["']`)

type pyprojectDetector struct{}

func (pyprojectDetector) Detect(path string) (*ProjectInfo, error) {
	f := filepath.Join(path, pyprojectToml)
	file, err := os.Open(f)
	if err != nil {
		return nil, nil
	}

	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("error closing file: %v", err)
		}
	}()

	inProject := false
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inProject = trimmed == "[project]"
			continue
		}
		if inProject {
			if m := pyprojectNameRe.FindStringSubmatch(line); len(m) == 2 && m[1] != "" {
				return &ProjectInfo{Name: m[1]}, nil
			}
		}
	}

	return nil, nil
}
