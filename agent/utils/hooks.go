package utils

import "strings"

// IsGryphCommand checks if a command string is a gryph hook command.
func IsGryphCommand(cmd string) bool {
	return strings.HasPrefix(cmd, "gryph")
}
