package utils

import (
	"github.com/pmezard/go-difflib/difflib"
)

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
