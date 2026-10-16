package localauth

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/platform/account"
	"github.com/safedep/gryph/platform/internal/macos"
)

// proofKind names a proof of Authorization Services: the external form of
// an authorization that the client obtained the action for.
const proofKind = "macos-authorization"

// ruleMarker is in the comment of the right, so a later install finds the
// right of this version and does not write it again.
const ruleMarker = "gryph approval rule v1"

// ruleContent is the right in the policy database. The person enters the
// password of the account that owns the login session, every time:
// shared off keeps the credential in the one authorization that asked,
// and it lasts 30 seconds, long enough for the service to check it. Root
// gets no pass, because root does not answer an approval request.
const ruleContent = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>class</key>
	<string>user</string>
	<key>authenticate-user</key>
	<true/>
	<key>session-owner</key>
	<true/>
	<key>shared</key>
	<false/>
	<key>timeout</key>
	<integer>30</integer>
	<key>allow-root</key>
	<false/>
	<key>comment</key>
	<string>` + ruleMarker + `: answer an approval request of Gryph</string>
	<key>default-prompt</key>
	<dict>
		<key></key>
		<string>Gryph needs your password to record your answer to an approval request.</string>
	</dict>
</dict>
</plist>
`

// security runs the security command line with stdin and returns its
// output. Tests replace it.
var security = func(ctx context.Context, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "security", args...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("security %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// lookupName returns the account name of uid. Tests replace it.
var lookupName = func(uid uint32) (string, error) {
	acct, err := account.LookupID(strconv.FormatUint(uint64(uid), 10))
	return acct.Name, err
}

// authorizationServices is the authority of macOS. It cannot ask on
// behalf of the service: the prompt shows only in the login session of
// the person. The client obtains the action and sends the external form,
// and the service checks it.
type authorizationServices struct{}

func platformAuthorizer() Authorizer { return authorizationServices{} }

func hint() string {
	return "run the command in the desktop session of the approver, where macOS can show the password prompt"
}

// Available reports whether the policy database defines the action. A
// host without it would fall back to the default rule, which asks an
// administrator.
func (authorizationServices) Available(context.Context) bool {
	return macos.RightExists(ActionID)
}

// Authorize checks the proof of the client. The authorization of the
// proof must give the action without a question, and the person who
// authenticated must be the account of the peer. A missing or a stale
// proof is a challenge: the person could authenticate but did not.
func (authorizationServices) Authorize(_ context.Context, req Request) (Result, error) {
	if req.Proof.Kind != proofKind || len(req.Proof.Data) != macos.ExternalFormSize {
		return Result{Challenge: true}, nil
	}
	var form [macos.ExternalFormSize]byte
	copy(form[:], req.Proof.Data)
	auth, err := macos.AuthorizationFromExternal(form)
	if err != nil {
		log.Debugf("localauth: the proof names no live authorization: %v", err)
		return Result{Challenge: true}, nil
	}
	defer auth.Free()
	if err := auth.Obtain(req.Action, false); err != nil {
		if macos.IsStatus(err, macos.StatusDenied) || macos.IsStatus(err, macos.StatusInteractionNotAllowed) {
			return Result{Challenge: true}, nil
		}
		return Result{}, fmt.Errorf("localauth: %w", err)
	}
	user, err := auth.Username()
	if err != nil {
		return Result{}, fmt.Errorf("localauth: %w", err)
	}
	want, err := lookupName(req.UID)
	if err != nil {
		return Result{}, fmt.Errorf("localauth: account of uid %d: %w", req.UID, err)
	}
	if user != want {
		log.Warnf("localauth: %s authenticated for an answer of %s", user, want)
		return Result{}, nil
	}
	return Result{Authorized: true}, nil
}

// register writes the right with security authorizationdb. A right of
// this version stays as it is.
func register(ctx context.Context) (Registration, error) {
	reg := Registration{Path: "authorizationdb:" + ActionID}
	if current, err := security(ctx, "", "authorizationdb", "read", ActionID); err == nil && strings.Contains(current, ruleMarker) {
		return reg, nil
	}
	if _, err := security(ctx, ruleContent, "authorizationdb", "write", ActionID); err != nil {
		return reg, err
	}
	reg.Changed = true
	return reg, nil
}

func unregister(ctx context.Context) (Registration, error) {
	reg := Registration{Path: "authorizationdb:" + ActionID}
	if _, err := security(ctx, "", "authorizationdb", "read", ActionID); err != nil {
		return reg, nil
	}
	if _, err := security(ctx, "", "authorizationdb", "remove", ActionID); err != nil {
		return reg, err
	}
	reg.Changed = true
	return reg, nil
}

// prepare obtains the action now: macOS shows the password prompt in the
// login session of this process, and the call lasts until the person
// answers. The proof is the external form, and stop frees the
// authorization after the service checked it. A host without the right,
// a session that cannot show the prompt, or a dismissed prompt gives no
// proof, and the service then refuses or waives as its configuration
// says.
func prepare(_ context.Context, action string) (Proof, func(), error) {
	noop := func() {}
	if !macos.RightExists(action) {
		return Proof{}, noop, nil
	}
	auth, err := macos.NewAuthorization()
	if err != nil {
		return Proof{}, noop, err
	}
	if err := auth.Obtain(action, true); err != nil {
		auth.Free()
		switch {
		case macos.IsStatus(err, macos.StatusCanceled):
			log.Warnf("localauth: the password prompt was dismissed")
			return Proof{}, noop, nil
		case macos.IsStatus(err, macos.StatusInteractionNotAllowed):
			log.Warnf("localauth: this session cannot show the password prompt: %s", hint())
			return Proof{}, noop, nil
		case macos.IsStatus(err, macos.StatusDenied):
			return Proof{}, noop, nil
		}
		return Proof{}, noop, err
	}
	form, err := auth.External()
	if err != nil {
		auth.Free()
		return Proof{}, noop, err
	}
	return Proof{Kind: proofKind, Data: form[:]}, auth.Free, nil
}
