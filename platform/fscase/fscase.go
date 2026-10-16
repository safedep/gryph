// Package fscase says how the default file system of the platform compares
// names.
package fscase

import "runtime"

// FoldsCase reports whether the default file system of the platform treats
// two names that differ only in case as one name. NTFS on Windows and APFS
// on macOS do so by default. A volume that an administrator made case
// sensitive also answers true, so a rule that folds case there matches
// more paths than the volume has, never fewer.
func FoldsCase() bool {
	return runtime.GOOS == "windows" || runtime.GOOS == "darwin"
}
