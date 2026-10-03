//go:build !linux

package peercred

// LoginIdentity returns the audit login identity of the peer process. The
// platform gives none here, so an answer counts as a match.
func (p *Peer) LoginIdentity() (string, bool) {
	return "", false
}
