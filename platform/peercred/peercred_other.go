//go:build !linux && !darwin

package peercred

import "net"

func open(net.Conn) (*Peer, error) { return nil, ErrUnsupported }

func (p *Peer) close() error { return nil }

func sameProcess(*Peer) bool { return true }
