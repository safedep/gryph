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

// KindTamper marks an entry that the service turns into a tamper event in
// the system session of the account.
const KindTamper = "tamper"

// Write puts entry under root/<account>/, as a file that the writer alone
// can read. The root must exist: the service creates it, root-owned with
// the sticky bit, so an account writes only under its own directory. The
// account directory is created on first use when the root allows it. The
// write goes through a temporary name and a rename, so a reader never
// sees a partial entry.
func Write(root, account string, entry Entry) (string, error) {
	if root == "" {
		return "", errors.New("spool: no spool directory")
	}
	if _, err := os.Stat(root); err != nil {
		return "", fmt.Errorf("spool: %w", err)
	}
	dir := filepath.Join(root, account)
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
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
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return "", fmt.Errorf("spool: %w", err)
	}
	final := filepath.Join(dir, name)
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("spool: %w", err)
	}
	return final, nil
}
