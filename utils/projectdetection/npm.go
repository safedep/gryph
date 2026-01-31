package projectdetection

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const packageJSON = "package.json"

type npmDetector struct{}

func (npmDetector) Detect(path string) (*ProjectInfo, error) {
	f := filepath.Join(path, packageJSON)
	data, err := os.ReadFile(f)
	if err != nil {
		return nil, nil
	}
	var m struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, nil
	}
	if m.Name == "" {
		return nil, nil
	}
	return &ProjectInfo{Name: m.Name}, nil
}
