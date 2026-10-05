// Package localauth asks the operating system to authenticate a person
// behind a process: polkit on Linux. The decision service uses it for the
// local-auth approval channel, so an answer to an approval request needs
// the password of the approver, which an agent under the approver's
// account does not know. The package is a small interface over the
// platform, so another authority can replace polkit.
package localauth

import (
	"context"
	"errors"
)

// ActionID names the polkit action of an answer to an approval request.
const ActionID = "io.safedep.gryph.approve"

// ErrUnavailable says the platform has no authority to ask, or the
// authority is not running.
var ErrUnavailable = errors.New("localauth: no authentication authority on this host")

// Subject is the process whose owner authenticates. StartTime is the
// start time of the process in clock ticks since boot, so a later process
// with the same pid is not the subject.
type Subject struct {
	PID       int32
	StartTime uint64
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
	// Authorize asks whether the owner of subject may do action. With
	// interactive the authority asks the person through its agent, and
	// the call lasts until the person answers, cancels, or ctx ends.
	Authorize(ctx context.Context, subject Subject, action string, interactive bool) (Result, error)
}

// Default returns the authority of the platform.
func Default() Authorizer {
	return platformAuthorizer()
}

// SubjectOf returns the subject for a process by its pid.
func SubjectOf(pid int32) (Subject, error) {
	start, err := startTime(pid)
	if err != nil {
		return Subject{}, err
	}
	return Subject{PID: pid, StartTime: start}, nil
}

// PolicyFile returns the path and the content of the file that declares
// the action to the authority, or an empty path on a platform without
// one. The install writes it as root.
func PolicyFile() (string, []byte) {
	return policyFile()
}

// Agent runs an authentication agent for the calling process, so the
// authority can ask on the terminal of this command. Stop ends it. On a
// platform or a host without one, Agent returns a no-op.
func Agent(ctx context.Context) (stop func(), err error) {
	return startAgent(ctx)
}
