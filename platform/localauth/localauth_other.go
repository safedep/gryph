//go:build !linux && !darwin

package localauth

import "context"

type none struct{}

func platformAuthorizer() Authorizer { return none{} }

func (none) Available(context.Context) bool { return false }

func (none) Authorize(context.Context, Request) (Result, error) {
	return Result{}, ErrUnavailable
}

func hint() string { return "this platform has no authority that asks for the password" }

func register(context.Context) (Registration, error) { return Registration{}, nil }

func unregister(context.Context) (Registration, error) { return Registration{}, nil }

func prepare(context.Context, string, string) (Proof, func(), error) {
	return Proof{}, func() {}, nil
}
