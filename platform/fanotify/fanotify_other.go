//go:build !linux

package fanotify

import "context"

type watcher struct{}

func available() error { return ErrUnsupported }

func open(Options) (*watcher, error) { return nil, ErrUnsupported }

func (*watcher) serve(context.Context, func(Request) bool, func(Change)) error {
	return ErrUnsupported
}

func (*watcher) close() error { return nil }

func (*watcher) remark(string) error { return ErrUnsupported }
