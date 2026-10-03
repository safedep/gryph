package procs

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// running reads the state field of /proc/<pid>/stat. The state follows the
// closing parenthesis of the name, so a name with spaces does not move it.
func running(pid int) bool {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	s := string(data)
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 >= len(s) {
		return false
	}
	switch s[i+2] {
	case 'Z', 'X':
		return false
	}
	return true
}
