package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/selfprotect"
)

// ProtectionProvider returns the provider that assesses this install in the
// user scope.
func (a *Runtime) ProtectionProvider() selfprotect.Provider {
	assets := selfprotect.UserAssets{
		RulesOff:    rulesOff(a.Config),
		Binary:      gryphBinaryPath(),
		ConfigFile:  a.Paths.ConfigFile,
		PolicyFile:  config.DefaultPolicyFilePath(a.Paths),
		PoliciesDir: config.DefaultPolicyDirPath(a.Paths),
		Store:       a.Config.GetDatabasePath(),
		Keys:        []string{a.Config.ResolveReceiptKeyPath(a.Paths), a.Config.ExportKeyFile()},
	}
	switch managed := config.ManagedConfigStatus(); {
	case managed.Exists && managed.Err == nil:
		assets.ConfigFile = managed.Path
		assets.ConfigManaged = true
	case managed.Exists:
		assets.ConfigDrift = "managed file ignored: " + managed.Err.Error()
	}
	for _, adapter := range a.Registry.All() {
		assets.HookConfigs = append(assets.HookConfigs, hookConfigAssessor{adapter: adapter})
	}
	return selfprotect.NewUserProvider(assets)
}

// rulesOff names the config key that keeps the built-in rules from running,
// or "" when they run. The rules are part of the policy, so a disabled
// policy turns them off too.
func rulesOff(cfg *config.Config) string {
	switch {
	case cfg == nil || !cfg.Policy.Enabled:
		return "policy.enabled"
	case !SelfProtectionEnabled(cfg):
		return "policy.self_protection.enabled"
	}
	return ""
}

// gryphBinaryPath returns the absolute path of the running binary, or ""
// when the program is not gryph. Tests override it.
var gryphBinaryPath = func() string {
	if cmd := utils.GryphCommand(); filepath.IsAbs(cmd) {
		return cmd
	}
	return ""
}

// hookConfigAssessor reads one agent's hook entries through its adapter.
type hookConfigAssessor struct {
	adapter agent.Adapter
}

func (h hookConfigAssessor) Name() string { return h.adapter.Name() }

// RepairHookConfig implements selfprotect.HookConfigRepairer. It rewrites
// the Gryph entries with a forced install under the repair rules: no link
// in the path, no backup, and a file that does not parse stays as it is.
func (h hookConfigAssessor) RepairHookConfig(ctx context.Context) error {
	result, err := h.adapter.Install(ctx, agent.InstallOptions{Force: true, Repair: true})
	switch {
	case err != nil:
		return err
	case result != nil && result.Error != nil:
		return result.Error
	case result == nil || !result.Success:
		return errors.New("the install did not succeed")
	}
	return nil
}

// AssessHookConfig implements selfprotect.HookConfigAssessor. It runs no
// agent binary, because the version plays no part in the assessment.
func (h hookConfigAssessor) AssessHookConfig(ctx context.Context) selfprotect.HookConfigState {
	detection, err := h.adapter.Detect(utils.WithoutProgramExecution(ctx))
	if err != nil || detection == nil || !detection.Installed {
		return selfprotect.HookConfigState{}
	}
	state := selfprotect.HookConfigState{Present: true, Path: detection.ConfigPath}
	if state.Path == "" {
		state.Path = detection.HooksPath
	}

	status, err := h.adapter.Status(ctx)
	switch {
	case err != nil:
		state.Drift = "cannot read the hook configuration: " + err.Error()
	case status == nil || !status.Installed:
		state.Drift = "hooks not installed"
	case !status.Valid:
		state.Drift = "hooks are invalid"
		if len(status.Issues) > 0 {
			state.Drift += ": " + status.Issues[0]
		}
	default:
		if missing := agent.MissingPromptHooks(h.adapter.Hooks(), status.Hooks); len(missing) > 0 {
			state.Drift = "prompt hook not installed: " + strings.Join(missing, ", ")
		}
	}
	return state
}
