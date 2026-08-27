//go:build windows

package config

import "os"

// Windows has no owner check without ACL inspection, so the file is
// accepted. This matches PMG. An ACL check must land before the managed
// directory carries policy, because standard users can create directories
// under %PROGRAMDATA%.
func verifyManagedFileTrust(_ os.FileInfo) bool {
	return true
}
