package projectdetection

// Detector detects project info from a directory (e.g. by parsing manifest files).
// Success: return (info, nil) with non-empty info.Name.
// Not applicable / no manifest: return (nil, nil).
// Error: return (nil, err); registry skips and tries next (best-effort).
type Detector interface {
	Detect(path string) (*ProjectInfo, error)
}
