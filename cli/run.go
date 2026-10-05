package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/engine"
	"github.com/safedep/gryph/platform/landlock"
	"github.com/spf13/cobra"
)

// RunEnv is the variable the launcher sets in the environment of the
// agent, so the census can tell an agent that started through it.
const RunEnv = engine.RunEnv

// NewRunCmd starts an agent under a Landlock ruleset that keeps the Gryph
// configuration directory read-only for the agent and everything it
// starts. The policy, the configuration and the keys are then out of the
// agent's write reach whatever path it takes: a shell, a script, an
// interpreter. The ruleset holds for the life of the process tree and
// cannot be lifted from inside it.
func NewRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <agent|program> [args...]",
		Short: "Start an agent with the Gryph configuration out of its write reach",
		Long: `Start an agent under a Landlock ruleset (Linux 5.13 or later, no root).
The configuration directory of Gryph, with the policy, the configuration
file and the keys, stays readable and takes no write from the agent and
from anything it starts. The rest of the file system stays as it is, with
one exception that the kernel imposes: the agent cannot create a new entry
directly inside an ancestor of the protected directory, such as the home
directory or ~/.config. It can create anything inside the entries that
exist there.

The first argument is an agent name, such as claude-code, or a program on
PATH. The remaining arguments go to the program. The launcher replaces
itself with the program.`,
		Example: `  gryph run claude-code
  gryph run claude-code --resume
  gryph run codex`,
		Args:               cobra.MinimumNArgs(1),
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
				return cmd.Help()
			}
			if _, err := landlock.ABI(); err != nil {
				return WrapError(ExitGeneral, "gryph run needs Landlock", err)
			}
			cfg, err := config.Load(globalFlags.ConfigPath)
			if err != nil {
				return ErrConfig("failed to load configuration", err)
			}
			paths := config.ResolvePaths()
			program, err := resolveProgram(args[0], cfg)
			if err != nil {
				return WrapError(ExitGeneral, "gryph run", err)
			}
			// A rule needs an existing path. Without the directory the
			// agent could make it and put a configuration of its own
			// there, so the launcher makes it first.
			if err := os.MkdirAll(paths.ConfigDir, 0o700); err != nil {
				return WrapError(ExitGeneral, "gryph run", err)
			}
			opts := landlock.Options{ReadOnly: []string{paths.ConfigDir}}
			if err := landlock.Restrict(opts); err != nil {
				return WrapError(ExitGeneral, "gryph run", err)
			}
			env := append(os.Environ(), RunEnv+"=1")
			argv := append([]string{program}, args[1:]...)
			if err := syscall.Exec(program, argv, env); err != nil {
				return WrapError(ExitGeneral, "start "+program, err)
			}
			return nil
		},
	}
	return cmd
}

// resolveProgram turns an agent name into its program, or finds the
// program on PATH.
func resolveProgram(name string, cfg *config.Config) (string, error) {
	registry := agent.NewRegistry()
	engine.RegisterAdapters(registry, nil, cfg)
	candidate := name
	if adapter, ok := registry.Get(name); ok {
		namer, ok := adapter.(agent.ProcessNamer)
		if !ok || len(namer.ProcessNames()) == 0 {
			return "", fmt.Errorf("%s has no program to start", name)
		}
		candidate = namer.ProcessNames()[0]
	}
	path, err := exec.LookPath(candidate)
	if err != nil {
		if strings.ContainsRune(candidate, '/') {
			return "", err
		}
		return "", errors.New(candidate + " is not on PATH")
	}
	return path, nil
}
