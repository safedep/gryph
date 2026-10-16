// Package localauth asks the operating system to authenticate a person
// behind a process: polkit on Linux, Authorization Services on macOS. The
// decision service uses it for the local-auth approval channel, so an
// answer to an approval request needs the password of the approver, which
// an agent under the approver's account does not know. The package is a
// small interface over the platform, so another authority can replace
// either one.
//
// An authority works in one of two ways. Polkit asks the person on behalf
// of the service: the service names the process and polkit finds its
// session. Another authority can ask only in the session of the person,
// so the client asks first and brings a Proof that the service checks.
package localauth

import (
	"context"
	"errors"
)

// ActionID names the action of an answer to an approval request.
const ActionID = "io.safedep.gryph.approve"

// ErrUnavailable says the platform has no authority to ask, or the
// authority is not running.
var ErrUnavailable = errors.New("localauth: no authentication authority on this host")

// Proof is what a client brings to show that a person authenticated in
// its session. Kind names the authority that made the proof, so a service
// never reads the proof of one authority as another. A zero Proof is no
// proof: the service asks the authority itself, or refuses.
type Proof struct {
	Kind string
	Data []byte
}

// Request is one question to the authority.
type Request struct {
	// PID and UID name the process that answers and its account, as the
	// kernel reported them at accept.
	PID int32
	UID uint32
	// StartTime is the start stamp of the process that peercred read at
	// accept, in the unit of the platform. Zero lets the authority find
	// it.
	StartTime uint64
	Action    string
	// Interactive lets the authority ask the person through its agent.
	// The call then lasts until the person answers or cancels. An
	// authority that asks on behalf of the service also ends with ctx.
	Interactive bool
	Proof       Proof
}

// Result is the answer of the authority.
type Result struct {
	// Authorized is true when the person authenticated.
	Authorized bool
	// Challenge is true when the person could authenticate but did not:
	// no agent asked, or the agent got no valid answer.
	Challenge bool
	// Dismissed is true when the person closed the prompt.
	Dismissed bool
}

// Authorizer asks the authority of the platform.
type Authorizer interface {
	// Available reports whether the authority runs on this host.
	Available(ctx context.Context) bool
	// Authorize asks whether the person behind the request may do its
	// action.
	Authorize(ctx context.Context, req Request) (Result, error)
}

// Default returns the authority of the platform.
func Default() Authorizer {
	return platformAuthorizer()
}

// Hint says what a person needs so that the authority can ask for the
// password, for a refusal of an answer that came without one.
func Hint() string {
	return hint()
}

// Registration names what Register or Unregister changed.
type Registration struct {
	// Path names the declaration of the action: a file, or an entry of
	// the database of the authority. It is empty on a platform without
	// one.
	Path    string
	Changed bool
}

// Register declares the action to the authority. The install calls it
// as root.
func Register(ctx context.Context) (Registration, error) {
	return register(ctx)
}

// Unregister removes the declaration of the action.
func Unregister(ctx context.Context) (Registration, error) {
	return unregister(ctx)
}

// Prepare readies the calling process for an answer that needs the
// password. It runs the agent of the authority for this terminal, or it
// asks the person now and returns the proof. prompt names what the
// password answers, for an authority that shows it. Stop ends what
// Prepare started, and the proof with it. On a platform or a host without
// an authority it returns no proof and a no-op stop.
func Prepare(ctx context.Context, action, prompt string) (proof Proof, stop func(), err error) {
	return prepare(ctx, action, prompt)
}
