package utils

import (
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// CountDiffLines computes the number of added and removed lines between old and new strings.
func CountDiffLines(oldStr, newStr string) (added, removed int) {
	if oldStr == newStr {
		return 0, 0
	}

	oldLines := difflib.SplitLines(oldStr)
	newLines := difflib.SplitLines(newStr)

	matcher := difflib.NewMatcher(oldLines, newLines)
	for _, op := range matcher.GetOpCodes() {
		switch op.Tag {
		case 'r':
			removed += op.I2 - op.I1
			added += op.J2 - op.J1
		case 'd':
			removed += op.I2 - op.I1
		case 'i':
			added += op.J2 - op.J1
		}
	}

	return added, removed
}

// CountNewFileLines counts lines in content for new file creation.
func CountNewFileLines(content string) int {
	if content == "" {
		return 0
	}
	n := strings.Count(content, "\n")
	if !strings.HasSuffix(content, "\n") {
		n++
	}
	return n
}

// GenerateDiff produces a unified diff string from old and new content for a given file path.
func GenerateDiff(filePath, oldStr, newStr string) string {
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(oldStr),
		B:        difflib.SplitLines(newStr),
		FromFile: "a/" + filePath,
		ToFile:   "b/" + filePath,
		Context:  3,
	})
	if err != nil {
		return ""
	}
	return diff
}
