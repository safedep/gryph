package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/engine"
	"github.com/safedep/gryph/platform/account"
	"github.com/spf13/cobra"
)

// keysRotateReport is the JSON contract of gryph supervisor keys rotate.
type keysRotateReport struct {
	KeyID      string `json:"key_id"`
	ReceiptKey string `json:"receipt_key"`
	TrustStore string `json:"trust_store"`
	// Owner is the account that owns the key after the rotation.
	Owner string `json:"owner"`
	// Reloaded is true when the running service took the new key. PID is
	// the service that got the signal. Note says why there was no reload.
	Reloaded bool   `json:"reloaded"`
	PID      int    `json:"pid,omitempty"`
	Note     string `json:"note,omitempty"`
}

func newSupervisorKeysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage the machine keys of the decision service",
	}
	cmd.AddCommand(newSupervisorKeysRotateCmd())
	return cmd
}

func newSupervisorKeysRotateCmd() *cobra.Command {
	var (
		stateDir string
		note     string
		noReload bool
		asJSON   bool
	)
	cmd := &cobra.Command{
		Use:   "rotate",
		Short: "Replace the receipt signing key of the decision service",
		Long: `Replace the receipt signing key of the decision service with a new one.

The command runs as root. It writes the new private key where the service
reads it, hands it to the owner of the state directory, which is the
service account, puts the public half in the managed trust store next to
the keys already there, and asks the running service to reload. The old
public key stays in the trust store, so the receipts it signed still
verify.`,
		Example: `  sudo gryph supervisor keys rotate
  sudo gryph supervisor keys rotate --note "quarterly rotation" --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !utils.IsPrivileged() {
				return NewCLIError(ExitGeneral, "supervisor keys rotate needs root: the trust store is the system's")
			}
			cfg, err := config.Load(globalFlags.ConfigPath)
			if err != nil {
				return ErrConfig("failed to load configuration", err)
			}
			if stateDir != "" {
				cfg.Supervisor.StateDir = stateDir
			}
			report, err := rotateMachineKey(cfg, note, !noReload)
			if err != nil {
				return WrapError(ExitGeneral, "rotate the machine key", err)
			}
			return renderKeysRotate(cmd.OutOrStdout(), report, asJSON)
		},
	}
	cmd.Flags().StringVar(&stateDir, "state-dir", "", "the state directory of the service instead of the configured one")
	cmd.Flags().StringVar(&note, "note", "", "a note stored with the key")
	cmd.Flags().BoolVar(&noReload, "no-reload", false, "write the key and the trust store, and leave the running service on the old key until it restarts")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON")
	return cmd
}

// rotateMachineKey is the rotation: a new key under the state directory,
// the public half in the managed trust store, the key handed to the
// service account, and a reload of the running service.
func rotateMachineKey(cfg *config.Config, note string, reload bool) (*keysRotateReport, error) {
	keys, err := engine.RotateMachineKey(cfg, note)
	if err != nil {
		return nil, err
	}
	owner, err := handMachineKeysToServiceAccount(cfg)
	if err != nil {
		return nil, err
	}
	if _, _, err := engine.TrustMachineKey(cfg, config.ManagedTrustStorePath()); err != nil {
		return nil, fmt.Errorf("trust store: %w", err)
	}
	report := &keysRotateReport{KeyID: keys.KeyID, ReceiptKey: keys.ReceiptKey, TrustStore: config.ManagedTrustStorePath(), Owner: owner}
	if !reload {
		report.Note = "the running service keeps the old key until it restarts"
		return report, nil
	}
	report.PID, report.Reloaded, report.Note = reloadService(cfg.Supervisor.PIDFile())
	return report, nil
}

// handMachineKeysToServiceAccount gives the key directory and its files to
// the owner of the state directory: the service account on a managed
// host. It returns the name of that account.
func handMachineKeysToServiceAccount(cfg *config.Config) (string, error) {
	info, err := os.Stat(cfg.Supervisor.StatePath())
	if err != nil {
		return "", err
	}
	uid, gid, ok := ownerOf(info)
	if !ok {
		return "", nil
	}
	for _, path := range []string{cfg.Supervisor.KeyDir(), cfg.Supervisor.ReceiptKeyPath(), cfg.Supervisor.PublicKeyPath(), cfg.Supervisor.ExportKeyPath()} {
		if err := os.Lchown(path, int(uid), int(gid)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("hand %s to uid %d: %w", path, uid, err)
		}
	}
	return accountName(uid), nil
}

// reloadService sends the reload signal to the pid in pidFile. It reports
// the pid, whether the signal went out, and the reason when it did not.
func reloadService(pidFile string) (pid int, reloaded bool, note string) {
	data, err := os.ReadFile(pidFile)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, "no running service found at " + pidFile + ": the next start takes the new key"
	}
	if err != nil {
		return 0, false, err.Error()
	}
	pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false, pidFile + " holds no pid: restart the service"
	}
	if err := reloadSignal(pid); err != nil {
		return pid, false, fmt.Sprintf("signal pid %d: %v: restart the service", pid, err)
	}
	return pid, true, ""
}

func renderKeysRotate(w io.Writer, report *keysRotateReport, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	lines := []string{
		"Rotated the machine key: " + report.KeyID,
		fmt.Sprintf("  %-12s %s  owner %s", "Private key", report.ReceiptKey, report.Owner),
		fmt.Sprintf("  %-12s %s", "Trust store", report.TrustStore),
	}
	switch {
	case report.Reloaded:
		lines = append(lines, fmt.Sprintf("  %-12s reloaded pid %d", "Service", report.PID))
	default:
		lines = append(lines, fmt.Sprintf("  %-12s %s", "Service", report.Note))
	}
	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
}

// accountName returns the name of uid, or the number when the account
// database does not know it.
func accountName(uid uint32) string {
	id := strconv.FormatUint(uint64(uid), 10)
	if a, err := account.LookupID(id); err == nil && a.Name != "" {
		return a.Name
	}
	return id
}
