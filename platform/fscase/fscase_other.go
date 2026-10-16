//go:build !windows && !darwin

package fscase

// FoldsCase reports false.
func FoldsCase() bool { return false }
