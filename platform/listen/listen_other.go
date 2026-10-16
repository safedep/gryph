//go:build !linux && !darwin

package listen

import "net"

func activated() ([]net.Listener, error) { return nil, nil }
