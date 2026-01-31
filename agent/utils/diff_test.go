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
