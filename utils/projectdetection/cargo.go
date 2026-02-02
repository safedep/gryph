package projectdetection

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const cargoToml = "Cargo.toml"

var cargoNameRe = regexp.MustCompile(`^\s*name\s*=\s*["']([^"']+)["']`)

type cargoDetector struct{}

func (cargoDetector) Detect(path string) (*ProjectInfo, error) {
	f := filepath.Join(path, cargoToml)
	file, err := os.Open(f)
	if err != nil {
		return nil, nil
	}

	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("error closing file: %v", err)
		}
	}()

	inPackage := false
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inPackage = trimmed == "[package]"
			continue
		}

		if inPackage {
			if m := cargoNameRe.FindStringSubmatch(line); len(m) == 2 && m[1] != "" {
				return &ProjectInfo{Name: m[1]}, nil
			}
		}
	}

	return nil, nil
}
