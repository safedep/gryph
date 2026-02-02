package projectdetection

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const goMod = "go.mod"

type gomodDetector struct{}

func (gomodDetector) Detect(path string) (*ProjectInfo, error) {
	f := filepath.Join(path, goMod)
	file, err := os.Open(f)
	if err != nil {
		return nil, nil
	}

	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("error closing file: %v", err)
		}
	}()

	sc := bufio.NewScanner(file)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "module ") {
			module := strings.TrimSpace(strings.TrimPrefix(line, "module "))
			module = strings.Trim(module, "\"`")
			if module == "" {
				return nil, nil
			}
			name := filepath.Base(module)
			if name == "." || name == ".." {
				return nil, nil
			}
			return &ProjectInfo{Name: name}, nil
		}
	}

	return nil, nil
}
