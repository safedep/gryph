//go:build !linux

package listen

import "net"

func activated() ([]net.Listener, error) { return nil, nil }
