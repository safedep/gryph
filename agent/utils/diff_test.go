package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateDiff_EditReplace(t *testing.T) {
	old := "line1\nline2\nline3\n"
	new := "line1\nchanged\nline3\n"

	diff := GenerateDiff("src/main.go", old, new)

	assert.Contains(t, diff, "--- a/src/main.go")
	assert.Contains(t, diff, "+++ b/src/main.go")
	assert.Contains(t, diff, "-line2")
	assert.Contains(t, diff, "+changed")
	assert.Contains(t, diff, " line1")
	assert.Contains(t, diff, " line3")
}

func TestGenerateDiff_NewFile(t *testing.T) {
	diff := GenerateDiff("new.go", "", "package main\n")

	assert.Contains(t, diff, "--- a/new.go")
	assert.Contains(t, diff, "+++ b/new.go")
	assert.Contains(t, diff, "+package main")
}

func TestGenerateDiff_EmptyInputs(t *testing.T) {
	diff := GenerateDiff("file.go", "", "")
	assert.Empty(t, diff)
}

func TestGenerateDiff_IdenticalContent(t *testing.T) {
	content := "same\ncontent\n"
	diff := GenerateDiff("file.go", content, content)
	assert.Empty(t, diff)
}

func TestGenerateDiff_MultilineEdit(t *testing.T) {
	old := "func main() {\n\tfmt.Println(\"hello\")\n}\n"
	new := "func main() {\n\tfmt.Println(\"world\")\n\tfmt.Println(\"!\")\n}\n"

	diff := GenerateDiff("main.go", old, new)

	assert.Contains(t, diff, "-\tfmt.Println(\"hello\")")
	assert.Contains(t, diff, "+\tfmt.Println(\"world\")")
	assert.Contains(t, diff, "+\tfmt.Println(\"!\")")
}

func TestCountDiffLines(t *testing.T) {
	tests := []struct {
		name        string
		oldStr      string
		newStr      string
		wantAdded   int
		wantRemoved int
	}{
		{
			name:        "empty to content",
			oldStr:      "",
			newStr:      "line1\nline2\nline3\n",
			wantAdded:   3,
			wantRemoved: 0,
		},
		{
			name:        "edit with additions",
			oldStr:      "line1\nline2\n",
			newStr:      "line1\nline2\nline3\nline4\n",
			wantAdded:   2,
			wantRemoved: 0,
		},
		{
			name:        "edit with deletions",
			oldStr:      "line1\nline2\nline3\n",
			newStr:      "line1\n",
			wantAdded:   0,
			wantRemoved: 2,
		},
		{
			name:        "mixed add and remove",
			oldStr:      "line1\nline2\nline3\n",
			newStr:      "line1\nchanged\nline3\nnew\n",
			wantAdded:   2,
			wantRemoved: 1,
		},
		{
			name:        "empty to empty",
			oldStr:      "",
			newStr:      "",
			wantAdded:   0,
			wantRemoved: 0,
		},
		{
			name:        "identical content",
			oldStr:      "same\ncontent\n",
			newStr:      "same\ncontent\n",
			wantAdded:   0,
			wantRemoved: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			added, removed := CountDiffLines(tt.oldStr, tt.newStr)
			assert.Equal(t, tt.wantAdded, added, "added lines")
			assert.Equal(t, tt.wantRemoved, removed, "removed lines")
		})
	}
}

func TestCountNewFileLines(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
	}{
		{name: "empty", content: "", want: 0},
		{name: "single line with newline", content: "hello\n", want: 1},
		{name: "single line without newline", content: "hello", want: 1},
		{name: "multiple lines", content: "a\nb\nc\n", want: 3},
		{name: "multiple lines no trailing newline", content: "a\nb\nc", want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CountNewFileLines(tt.content))
		})
	}
}
