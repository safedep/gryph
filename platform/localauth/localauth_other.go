//go:build !linux

package localauth

import "context"

type none struct{}

func platformAuthorizer() Authorizer { return none{} }

func (none) Available(context.Context) bool { return false }

func (none) Authorize(context.Context, Subject, string, bool) (Result, error) {
	return Result{}, ErrUnavailable
}

func policyFile() (string, []byte) { return "", nil }

func startTime(int32) (uint64, error) { return 0, ErrUnavailable }

func startAgent(context.Context) (func(), error) { return func() {}, nil }
