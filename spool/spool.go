// Package spool is the drop directory of the hook clients. A client that
// cannot reach the decision service, or that found a tamper event, leaves
// an entry under its own account's directory. The service reads the
// entries later and assigns them to the partition of the directory owner,
// never to an account named in the entry.
package spool

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/safedep/gryph/decision/ipc"
)

// Entry is one spooled item: the frame the client could not send, the
// verdict it gave in its place, and why.
type Entry struct {
	RecordedAt time.Time `json:"recorded_at"`
	// Kind is empty for an action the client decided without the service,
	// and KindTamper for a tamper event the client found itself.
	Kind    string     `json:"kind,omitempty"`
	Verdict string     `json:"verdict,omitempty"`
	Reason  string     `json:"reason"`
	Frame   *ipc.Frame `json:"frame,omitempty"`
}

// The kinds of an entry.
const (
	// KindTamper marks an entry that the service turns into a tamper event
	// in the system session of the account.
	KindTamper = "tamper"
	// KindDegraded marks an action that the client evaluated itself in
	// local-ephemeral mode: the managed policy with no session context, no
	// receipt and no signature.
	KindDegraded = "degraded"
)

// Degraded reports whether the client decided the entry without the
// service: the service was out of reach, or the client evaluated the
// policy itself.
func (e Entry) Degraded() bool {
	return e.Kind == "" || e.Kind == KindDegraded
}

// The modes of the spool. The root carries the set-group-ID bit and the
// sticky bit: an account directory made under it takes the group of the
// root, which is the service account's, and no account removes another
// account's entries. The account directory and its files give that group
// read, and the directory write, so the service reads and removes the
// entries without a privilege. The modes are set after the create, so the
// umask of the writer does not take the group bits away.
const (
	RootMode = os.ModeSetgid | os.ModeSticky | 0o733
	DirMode  = os.FileMode(0o770)
	FileMode = os.FileMode(0o640)
)

// Write puts entry under root/<account>/, as a file that the writer and
// the service can read. The root must exist: the service creates it, so
// an account writes only under its own directory. The account directory is
// created on first use when the root allows it. The write goes through a
// temporary name and a rename, so a reader never sees a partial entry.
func Write(root, account string, entry Entry) (string, error) {
	if root == "" {
		return "", errors.New("spool: no spool directory")
	}
	if _, err := os.Stat(root); err != nil {
		return "", fmt.Errorf("spool: %w", err)
	}
	dir := filepath.Join(root, account)
	switch err := os.Mkdir(dir, DirMode); {
	case err == nil:
		if err := os.Chmod(dir, DirMode); err != nil {
			return "", fmt.Errorf("spool: %w", err)
		}
	case !errors.Is(err, os.ErrExist):
		return "", fmt.Errorf("spool: %w", err)
	}
	if entry.RecordedAt.IsZero() {
		entry.RecordedAt = time.Now()
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return "", err
	}
	name := strconv.FormatInt(entry.RecordedAt.UnixNano(), 10) + "-" + strconv.Itoa(os.Getpid()) + ".json"
	tmp := filepath.Join(dir, "."+name+".tmp")
	if err := os.WriteFile(tmp, data, FileMode); err != nil {
		return "", fmt.Errorf("spool: %w", err)
	}
	if err := os.Chmod(tmp, FileMode); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("spool: %w", err)
	}
	final := filepath.Join(dir, name)
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("spool: %w", err)
	}
	return final, nil
}

// EnsureRoot creates the root when it is missing, with RootMode. An
// existing root keeps its mode: the packaging of the host owns it.
func EnsureRoot(root string) error {
	if root == "" {
		return errors.New("spool: no spool directory")
	}
	if _, err := os.Stat(root); err == nil {
		return nil
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("spool: %w", err)
	}
	if err := os.Chmod(root, RootMode); err != nil {
		return fmt.Errorf("spool: %w", err)
	}
	return nil
}
