package fscase

// FoldsCase reports true: APFS folds case by default. On a volume that an
// administrator made case sensitive, a rule that folds case matches more
// paths, never fewer.
func FoldsCase() bool { return true }
