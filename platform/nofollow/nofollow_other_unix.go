//go:build !windows && !linux

package nofollow

// openBeneath opens the directory comps below h one component at a time.
func (h dirHandle) openBeneath(comps []string) (dirHandle, error) {
	return h.walk(comps)
}
