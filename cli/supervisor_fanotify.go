package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/engine"
	"github.com/safedep/gryph/platform/account"
	"github.com/safedep/gryph/platform/fanotify"
	"github.com/safedep/gryph/selfprotect"
	"github.com/spf13/cobra"
)

// fanotifyChangeLimit bounds the changes the state file keeps.
const fanotifyChangeLimit = 32

// newSupervisorFanotifyCmd is the kernel watcher: a process that stops a
// write to the managed files and the binary by a process of a
// non-privileged account, with the permission events of fanotify. It is
// not part of the decision service and talks to nobody. It writes a state
// file that gryph doctor reads: the paths it marks, and the changes it
// noticed and could not stop.
func newSupervisorFanotifyCmd() *cobra.Command {
	var (
		stateFile string
		pidFile   string
		allowRoot bool
	)
	cmd := &cobra.Command{
		Use:   "fanotify",
		Short: "Stop a write to the managed files by a non-privileged account",
		Long: `Stop a write to the managed files and the binary by a process of a
non-privileged account, with the permission events of the Linux fanotify
API. A read passes. Root and the service account pass. A rename or an
unlink has no permission event: the watcher records it in its state file,
which gryph doctor reads.

The watcher needs CAP_SYS_ADMIN for the group and CAP_SYS_PTRACE to read
the open flags of the process that asks. It runs as the service account
with those two capabilities, in its own unit, never as root: --allow-root
exists for a test.`,
		Example: `  gryph supervisor fanotify
  gryph supervisor fanotify --state-file /tmp/fanotify.json --pid-file /tmp/fanotify.pid --allow-root`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if utils.IsPrivileged() && !allowRoot {
				return NewCLIError(ExitGeneral, "supervisor fanotify must not run as root: run it as the service account with CAP_SYS_ADMIN and CAP_SYS_PTRACE, or pass --allow-root for a test")
			}
			if err := fanotify.Available(); err != nil {
				return WrapError(ExitGeneral, "fanotify", err)
			}
			if !config.ManagedConfigActive() {
				return NewCLIError(ExitConfig, "no managed configuration: nothing to protect")
			}
			cfg, err := config.Load(globalFlags.ConfigPath)
			if err != nil {
				return ErrConfig("failed to load configuration", err)
			}
			if stateFile == "" {
				stateFile = cfg.Supervisor.FanotifyStatePath()
			}
			if pidFile == "" {
				pidFile = filepath.Join(filepath.Dir(stateFile), "fanotify.pid")
			}
			opts := protectedPaths(cfg)
			if len(opts.Files) == 0 && len(opts.Dirs) == 0 {
				return NewCLIError(ExitConfig, "no managed file exists to protect")
			}
			if acct, err := account.Lookup(cfg.Supervisor.ServerAccount()); err == nil {
				opts.ExemptUIDs = append(opts.ExemptUIDs, acct.UID)
			}
			w, err := fanotify.Open(opts)
			if err != nil {
				return WrapError(ExitGeneral, "fanotify", err)
			}
			defer func() { _ = w.Close() }()

			keeper := &fanotifyState{path: stateFile, state: selfprotect.FanotifyState{PID: os.Getpid(), Started: time.Now().UTC(), Files: opts.Files, Dirs: opts.Dirs, Changes: []selfprotect.FanotifyChange{}}}
			if err := keeper.write(); err != nil {
				return WrapError(ExitGeneral, "write the state file", err)
			}
			defer func() { _ = os.Remove(stateFile) }()
			if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
				log.Warnf("fanotify: pid file not written: %v", err)
			} else {
				defer func() { _ = os.Remove(pidFile) }()
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			log.Infof("fanotify: watching %d file(s) and %d directory(ies)", len(opts.Files), len(opts.Dirs))
			return w.Serve(ctx, func(r fanotify.Request) bool {
				ok := fanotify.Decide(opts.ExemptUIDs, r)
				if !ok {
					keeper.denied()
					log.Infof("fanotify: denied an open of %s by pid %d uid %d", r.Path, r.PID, r.UID)
				}
				return ok
			}, func(c fanotify.Change) {
				keeper.change(c)
				// A rename or an unlink leaves the mark on the old inode. The
				// path, when it exists again, is a new file that needs a mark
				// of its own.
				if (c.Op == "deleted" || c.Op == "moved") && slices.Contains(opts.Files, c.Path) {
					if err := w.Remark(c.Path); err != nil {
						log.Warnf("fanotify: %s is not protected after a %s: %v", c.Path, c.Op, err)
					}
				}
			})
		},
	}
	cmd.Flags().StringVar(&stateFile, "state-file", "", "report the watched paths and the changes here instead of the configured file")
	cmd.Flags().StringVar(&pidFile, "pid-file", "", "write the pid here instead of next to the state file")
	cmd.Flags().BoolVar(&allowRoot, "allow-root", false, "allow a run as root, for a test")
	return cmd
}

// protectedPaths names what the watcher marks: the managed directory and
// its files, the policies and the keys directories, the binary the hook
// entries name, and the managed hook file of every agent with a locked
// entry. A path that does not exist is left out, because a mark needs an
// inode.
func protectedPaths(cfg *config.Config) fanotify.Options {
	var opts fanotify.Options
	file := func(path string) {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			opts.Files = append(opts.Files, path)
		}
	}
	dir := func(path string) {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			opts.Dirs = append(opts.Dirs, path)
		}
	}
	managed := config.ManagedConfigDir()
	dir(managed)
	dir(filepath.Join(managed, "policies"))
	dir(filepath.Join(managed, "keys"))
	file(config.ManagedConfigPath())
	file(filepath.Join(managed, "policy.yaml"))
	file(config.ManagedBinaryPath(cfg))
	registry := agent.NewRegistry()
	engine.RegisterAdapters(registry, nil, cfg)
	for _, name := range cfg.Managed.Agents {
		a, ok := registry.Get(name)
		if !ok {
			continue
		}
		installer, ok := a.(agent.ManagedInstaller)
		if ok && installer.ManagedClass() == agent.ManagedClassLocked {
			file(installer.ManagedHookPath())
		}
	}
	return opts
}

// fanotifyState keeps the state file current. Every change and every
// refusal rewrites it, through a temporary file and a rename, so a reader
// never sees a partial file.
type fanotifyState struct {
	path  string
	mu    sync.Mutex
	state selfprotect.FanotifyState
}

func (k *fanotifyState) denied() {
	k.mu.Lock()
	k.state.Denied++
	k.mu.Unlock()
	if err := k.write(); err != nil {
		log.Warnf("fanotify: state file not written: %v", err)
	}
}

func (k *fanotifyState) change(c fanotify.Change) {
	k.mu.Lock()
	k.state.Changes = append(k.state.Changes, selfprotect.FanotifyChange{Time: c.Time, Op: c.Op, Path: c.Path, PID: c.PID, UID: c.UID})
	if len(k.state.Changes) > fanotifyChangeLimit {
		k.state.Changes = k.state.Changes[len(k.state.Changes)-fanotifyChangeLimit:]
	}
	k.mu.Unlock()
	if err := k.write(); err != nil {
		log.Warnf("fanotify: state file not written: %v", err)
	}
}

func (k *fanotifyState) write() error {
	k.mu.Lock()
	data, err := json.MarshalIndent(k.state, "", "  ")
	k.mu.Unlock()
	if err != nil {
		return err
	}
	tmp := k.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, k.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
