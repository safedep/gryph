package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/engine"
	"github.com/safedep/gryph/platform/account"
	"github.com/spf13/cobra"
)

// userHelperTimeout bounds one per-user helper run. A home on a slow
// network mount must not hold the uninstall of the whole host.
const userHelperTimeout = 60 * time.Second

// userHelperOutputLimit bounds what the parent reads from a helper. The
// helper runs as the user, so its output is the user's data.
const userHelperOutputLimit = 1 << 20

// managedUninstallReport is the JSON contract of gryph uninstall --managed.
type managedUninstallReport struct {
	Status  string `json:"status"`
	Changed bool   `json:"changed"`
	// Removed lists the managed files the run removed.
	Removed []string               `json:"removed"`
	Agents  []managedAgentReport   `json:"agents"`
	Users   []managedUserUninstall `json:"users"`
	// UsersSkipped says why no home was visited, when the platform cannot
	// list accounts or run a command as one.
	UsersSkipped string `json:"users_skipped,omitempty"`
}

// managedUserUninstall is the outcome of the helper for one account.
type managedUserUninstall struct {
	Name   string               `json:"name"`
	ID     string               `json:"id"`
	Home   string               `json:"home"`
	Agents []userAgentUninstall `json:"agents"`
	Purged []string             `json:"purged,omitempty"`
	Error  string               `json:"error,omitempty"`
}

// runManagedUninstall is gryph uninstall --managed: the reverse of
// install --managed, for an MDM script that runs as root. It removes the
// Gryph entries from every managed hook file, then the Gryph entries from
// the agent hook files in every account's home, then the managed policy
// and configuration. The per-user state stays unless --purge.
//
// The walk of the homes is the only place where a root process touches a
// user's home, and it never does so as root: a helper drops to the
// account's uid and gid before it opens a file, under the repair rules.
func runManagedUninstall(cmd *cobra.Command, purge, dryRun, asJSON bool) error {
	if !utils.IsPrivileged() {
		return NewCLIError(ExitGeneral, "uninstall --managed must run as root")
	}
	if config.ManagedConfigPath() == "" {
		return NewCLIError(ExitGeneral, "this platform has no system managed location")
	}
	ctx := utils.WithoutProgramExecution(context.Background())
	report := &managedUninstallReport{Status: managedStatusOK, Removed: []string{}, Agents: []managedAgentReport{}, Users: []managedUserUninstall{}}
	degraded := 0

	registry := agent.NewRegistry()
	engine.RegisterAdapters(registry, nil, config.Default())
	installers := map[string]agent.ManagedInstaller{}
	for _, a := range registry.All() {
		if m, ok := a.(agent.ManagedInstaller); ok {
			installers[a.Name()] = m
		}
	}
	for _, name := range sortedKeys(installers) {
		installer := installers[name]
		row := managedAgentReport{Name: name, Path: installer.ManagedHookPath(), Class: string(installer.ManagedClass()), Action: "remove"}
		res, err := installer.UninstallManaged(ctx, agent.ManagedInstallOptions{DryRun: dryRun})
		if err != nil {
			row.Error = err.Error()
			degraded++
		} else {
			row.Changed = res.Changed
		}
		report.Changed = report.Changed || row.Changed
		report.Agents = append(report.Agents, row)
	}

	degraded += walkHomes(ctx, report, purge, dryRun)

	policy := config.ManagedPolicyState()
	for _, path := range []string{policy.File, policy.Dir, config.ManagedConfigPath()} {
		removed, err := removeManagedPath(path, dryRun)
		if err != nil {
			return WrapError(ExitGeneral, "remove "+path, err)
		}
		if removed {
			report.Changed = true
			report.Removed = append(report.Removed, path)
		}
	}

	switch {
	case dryRun:
		report.Status = managedStatusDryRun
	case degraded > 0:
		report.Status = managedStatusPartial
	}
	if err := renderManagedUninstall(cmd.OutOrStdout(), report, asJSON); err != nil {
		return err
	}
	if degraded > 0 {
		return &exitError{code: ExitManagedPartial, message: fmt.Sprintf("uninstall --managed: %d item(s) degraded", degraded)}
	}
	return nil
}

// walkHomes runs the per-user helper for every human account and returns
// the count of accounts that reported a failure.
func walkHomes(ctx context.Context, report *managedUninstallReport, purge, dryRun bool) int {
	accounts, err := account.List()
	if err != nil {
		report.UsersSkipped = err.Error()
		return 0
	}
	self, err := os.Executable()
	if err != nil {
		report.UsersSkipped = "the running program has no path: " + err.Error()
		return 0
	}
	degraded := 0
	for _, acct := range accounts {
		row := managedUserUninstall{Name: acct.Name, ID: acct.ID, Home: acct.Home, Agents: []userAgentUninstall{}}
		helper, err := runUserHelper(ctx, self, acct, purge, dryRun)
		if err != nil {
			row.Error = err.Error()
			degraded++
		} else {
			row.Agents = helper.Agents
			row.Purged = helper.Purged
			for _, a := range helper.Agents {
				if a.Error != "" {
					degraded++
					break
				}
			}
			report.Changed = report.Changed || userChanged(helper)
		}
		report.Users = append(report.Users, row)
	}
	return degraded
}

func runUserHelper(ctx context.Context, self string, acct account.Account, purge, dryRun bool) (*userUninstallReport, error) {
	ctx, cancel := context.WithTimeout(ctx, userHelperTimeout)
	defer cancel()
	args := []string{"_uninstall-user"}
	if purge {
		args = append(args, "--purge")
	}
	if dryRun {
		args = append(args, "--dry-run")
	}
	cmd, err := account.Command(ctx, acct, self, args...)
	if err != nil {
		return nil, err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &stdout, n: userHelperOutputLimit}
	cmd.Stderr = &limitedWriter{w: &stderr, n: userHelperOutputLimit}
	if err := cmd.Run(); err != nil {
		msg := bytes.TrimSpace(stderr.Bytes())
		if len(msg) > 0 {
			return nil, fmt.Errorf("helper for %s: %w: %s", acct.Name, err, msg)
		}
		return nil, fmt.Errorf("helper for %s: %w", acct.Name, err)
	}
	var report userUninstallReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		return nil, fmt.Errorf("helper for %s printed no report: %w", acct.Name, err)
	}
	return &report, nil
}

func userChanged(r *userUninstallReport) bool {
	if len(r.Purged) > 0 {
		return true
	}
	for _, a := range r.Agents {
		if len(a.HooksRemoved) > 0 {
			return true
		}
	}
	return false
}

// removeManagedPath removes a managed file, or a managed directory that
// holds nothing, and refuses a link. It reports whether anything went.
func removeManagedPath(path string, dryRun bool) (bool, error) {
	if path == "" {
		return false, nil
	}
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	case err != nil:
		return false, err
	case info.Mode()&os.ModeSymlink != 0:
		return false, fmt.Errorf("%s is a symbolic link, not a managed file", path)
	case info.IsDir():
		entries, err := os.ReadDir(path)
		if err != nil {
			return false, err
		}
		if len(entries) > 0 {
			return false, nil
		}
	}
	if dryRun {
		return true, nil
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	// The managed directory itself goes when nothing is left in it, so a
	// host that never had Gryph and a host that removed it look the same.
	if dir := filepath.Dir(path); dir != "" {
		if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
			_ = os.Remove(dir)
		}
	}
	return true, nil
}

func renderManagedUninstall(w io.Writer, report *managedUninstallReport, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	lines := []string{"Managed uninstall: " + report.Status}
	for _, a := range report.Agents {
		state := "unchanged"
		if a.Changed {
			state = "changed"
		}
		line := fmt.Sprintf("  %-12s %-11s %-9s %s", a.Name, a.Class, state, a.Path)
		if a.Error != "" {
			line += "  error: " + a.Error
		}
		lines = append(lines, line)
	}
	for _, u := range report.Users {
		line := fmt.Sprintf("  user %s (%s) %s", u.Name, u.ID, u.Home)
		if u.Error != "" {
			line += "  error: " + u.Error
		}
		lines = append(lines, line)
		for _, a := range u.Agents {
			detail := fmt.Sprintf("%d hook(s) removed", len(a.HooksRemoved))
			if a.Error != "" {
				detail = "error: " + a.Error
			}
			lines = append(lines, fmt.Sprintf("    %-12s %s", a.Name, detail))
		}
		for _, p := range u.Purged {
			lines = append(lines, "    purged "+p)
		}
	}
	if report.UsersSkipped != "" {
		lines = append(lines, "  users skipped: "+report.UsersSkipped)
	}
	for _, p := range report.Removed {
		lines = append(lines, "  removed "+p)
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

// limitedWriter keeps the first n bytes and drops the rest.
type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return len(p), nil
	}
	keep := p
	if len(keep) > l.n {
		keep = keep[:l.n]
	}
	l.n -= len(keep)
	if _, err := l.w.Write(keep); err != nil {
		return 0, err
	}
	return len(p), nil
}
