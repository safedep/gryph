package localauth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/safedep/dry/log"
)

const (
	polkitName      = "org.freedesktop.PolicyKit1"
	polkitPath      = "/org/freedesktop/PolicyKit1/Authority"
	polkitInterface = "org.freedesktop.PolicyKit1.Authority"
	// polkitAllowUserInteraction is the CheckAuthorization flag that lets
	// polkit ask the person through an agent.
	polkitAllowUserInteraction uint32 = 1
	// agentStartTimeout bounds the registration of pkttyagent.
	agentStartTimeout = 5 * time.Second
)

// policyContent declares the action. auth_self asks the approver for
// their own password every time: the group check already said who may
// answer, and the password proves a person is at the keyboard.
const policyContent = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE policyconfig PUBLIC "-//freedesktop//DTD PolicyKit Policy Configuration 1.0//EN" "http://www.freedesktop.org/standards/PolicyKit/1/policyconfig.dtd">
<policyconfig>
  <vendor>SafeDep</vendor>
  <vendor_url>https://safedep.io</vendor_url>
  <action id="io.safedep.gryph.approve">
    <description>Answer an approval request of Gryph</description>
    <message>Authenticate to answer the approval request</message>
    <defaults>
      <allow_any>auth_self</allow_any>
      <allow_inactive>auth_self</allow_inactive>
      <allow_active>auth_self</allow_active>
    </defaults>
  </action>
</policyconfig>
`

// policyPath is where polkit reads action declarations. Tests replace it.
var policyPath = "/usr/share/polkit-1/actions/io.safedep.gryph.policy"

type polkit struct{}

func platformAuthorizer() Authorizer { return polkit{} }

func hint() string {
	return "run the command from a session with a polkit agent, or on a terminal where pkttyagent can ask"
}

// register writes the action file, readable by every account. The
// directory belongs to the polkit package.
func register(context.Context) (Registration, error) {
	reg := Registration{Path: policyPath}
	if current, err := os.ReadFile(policyPath); err == nil && bytes.Equal(current, []byte(policyContent)) {
		return reg, nil
	}
	if err := os.WriteFile(policyPath, []byte(policyContent), 0o644); err != nil {
		return reg, err
	}
	reg.Changed = true
	return reg, nil
}

func unregister(context.Context) (Registration, error) {
	reg := Registration{Path: policyPath}
	switch err := os.Remove(policyPath); {
	case err == nil:
		reg.Changed = true
	case !errors.Is(err, fs.ErrNotExist):
		return reg, err
	}
	return reg, nil
}

// Available reports whether the system bus answers and polkit owns, or
// can take, its name on it.
func (polkit) Available(ctx context.Context) bool {
	conn, err := dbus.SystemBus()
	if err != nil {
		return false
	}
	var owned bool
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, polkitName).Store(&owned); err != nil {
		return false
	}
	if owned {
		return true
	}
	var names []string
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ListActivatableNames", 0).Store(&names); err != nil {
		return false
	}
	for _, n := range names {
		if n == polkitName {
			return true
		}
	}
	return false
}

// Authorize calls CheckAuthorization on the process of the request. Polkit
// needs no proof: it finds the session of the process and asks there. The
// start time is the one peercred read at accept, in clock ticks since
// boot, so a later process with the same pid is not the subject. Zero
// lets polkit read it. The call lasts until the person answers. When ctx
// ends first, the call is cancelled through its cancellation id.
func (polkit) Authorize(ctx context.Context, req Request) (Result, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	obj := conn.Object(polkitName, polkitPath)
	details := map[string]dbus.Variant{
		"pid":        dbus.MakeVariant(uint32(req.PID)),
		"start-time": dbus.MakeVariant(req.StartTime),
	}
	var flags uint32
	cancellation := ""
	if req.Interactive {
		flags = polkitAllowUserInteraction
		cancellation = fmt.Sprintf("gryph-%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	// The reply is one struct: (is_authorized, is_challenge, details).
	type reply struct {
		Authorized bool
		Challenge  bool
		Details    map[string]string
	}
	done := make(chan struct{})
	var (
		out     reply
		callErr error
	)
	go func() {
		defer close(done)
		call := obj.Call(polkitInterface+".CheckAuthorization", 0, struct {
			Kind    string
			Details map[string]dbus.Variant
		}{"unix-process", details}, req.Action, map[string]string{}, flags, cancellation)
		if call.Err != nil {
			callErr = call.Err
			return
		}
		callErr = call.Store(&out)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		if cancellation != "" {
			if c := obj.Call(polkitInterface+".CancelCheckAuthorization", 0, cancellation); c.Err != nil {
				log.Debugf("localauth: cancel: %v", c.Err)
			}
		}
		<-done
		if callErr == nil {
			return Result{Challenge: true, Dismissed: true}, ctx.Err()
		}
	}
	if callErr != nil {
		var dbErr dbus.Error
		if errors.As(callErr, &dbErr) && strings.HasSuffix(dbErr.Name, ".ServiceUnknown") {
			return Result{}, fmt.Errorf("%w: %v", ErrUnavailable, callErr)
		}
		return Result{}, fmt.Errorf("localauth: CheckAuthorization: %w", callErr)
	}
	res := Result{Authorized: out.Authorized, Challenge: out.Challenge}
	if out.Details["polkit.dismissed"] != "" {
		res.Dismissed = true
	}
	return res, nil
}

// prepare runs pkttyagent for this process, so polkit asks on the
// terminal of the command while the service waits for the answer. Polkit
// makes no proof. The agent reports its registration on a notify
// descriptor. Without pkttyagent or a system bus there is nothing to
// start.
func prepare(ctx context.Context, _, _ string) (Proof, func(), error) {
	noop := func() {}
	path, err := exec.LookPath("pkttyagent")
	if err != nil {
		log.Warnf("localauth: no pkttyagent on PATH, the authority cannot ask on this terminal")
		return Proof{}, noop, nil
	}
	if !(polkit{}).Available(ctx) {
		log.Debugf("localauth: no authority on the system bus, nothing asks on this terminal")
		return Proof{}, noop, nil
	}
	r, w, err := os.Pipe()
	if err != nil {
		return Proof{}, noop, err
	}
	// No --fallback: polkit asks a fallback agent only when no other agent
	// serves the session, and a process with no session has none.
	cmd := exec.CommandContext(ctx, path, "--process", strconv.Itoa(os.Getpid()), "--notify-fd", "3")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.ExtraFiles = []*os.File{w}
	if err := cmd.Start(); err != nil {
		_ = r.Close()
		_ = w.Close()
		return Proof{}, noop, fmt.Errorf("start pkttyagent: %w", err)
	}
	_ = w.Close()
	// The agent closes the notify descriptor once it is registered.
	registered := make(chan struct{})
	go func() {
		buf := make([]byte, 1)
		_, _ = r.Read(buf)
		_ = r.Close()
		close(registered)
	}()
	select {
	case <-registered:
		log.Debugf("localauth: pkttyagent registered for pid %d", os.Getpid())
	case <-time.After(agentStartTimeout):
		log.Warnf("localauth: pkttyagent did not register in time")
	}
	return Proof{}, func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}, nil
}
