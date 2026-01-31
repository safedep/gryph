package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHashContent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty input returns empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "known content produces expected hash",
			input:    "hello world",
			expected: "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
		},
		{
			name:     "deterministic output",
			input:    "package main\n\nfunc main() {}\n",
			expected: HashContent("package main\n\nfunc main() {}\n"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HashContent(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHashContent_Determinism(t *testing.T) {
	content := "some file content with special chars: @#$%^&*()"
	hash1 := HashContent(content)
	hash2 := HashContent(content)
	assert.Equal(t, hash1, hash2)
	assert.Len(t, hash1, 64)
}
