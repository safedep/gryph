package opencode

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/safedep/gryph/agent"
)

//go:embed plugin.js
var pluginJS []byte

var HookTypes = []string{
	"tool.execute.before",
	"tool.execute.after",
	"session.created",
	"session.idle",
	"session.error",
}

const pluginFileName = "gryph.js"

func pluginPath(configPath string) string {
	return filepath.Join(configPath, "plugins", pluginFileName)
}

func InstallHooks(ctx context.Context, opts agent.InstallOptions) (*agent.InstallResult, error) {
	result := &agent.InstallResult{
		BackupPaths: make(map[string]string),
	}

	detection, err := Detect(ctx)
	if err != nil {
		result.Error = err
		return result, err
	}

	if !detection.Installed {
		result.Error = fmt.Errorf("OpenCode not detected (~/.config/opencode not found)")
		return result, result.Error
	}

	pluginFile := pluginPath(detection.ConfigPath)

	if _, err := os.Stat(pluginFile); err == nil && !opts.Force && !opts.DryRun {
		existing, readErr := os.ReadFile(pluginFile)
		if readErr == nil && bytes.Contains(existing, []byte("invokeGryph")) {
			result.Warnings = append(result.Warnings, "gryph plugin already installed (use --force to overwrite)")
			result.Success = true
			return result, nil
		}
	}

	if opts.Backup && !opts.DryRun {
		if _, err := os.Stat(pluginFile); err == nil {
			var backupPath string
			if opts.BackupDir != "" {
				backupDir := filepath.Join(opts.BackupDir, "opencode")
				if err := os.MkdirAll(backupDir, 0700); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("failed to create backup directory: %v", err))
				} else {
					backupPath = filepath.Join(backupDir, pluginFileName+".backup")
					if data, err := os.ReadFile(pluginFile); err == nil {
						if err := os.WriteFile(backupPath, data, 0600); err == nil {
							result.BackupPaths[pluginFileName] = backupPath
						}
					}
				}
			} else {
				backupPath = pluginFile + ".backup"
				if data, err := os.ReadFile(pluginFile); err == nil {
					if err := os.WriteFile(backupPath, data, 0600); err == nil {
						result.BackupPaths[pluginFileName] = backupPath
					}
				}
			}
		}
	}

	if opts.DryRun {
		result.HooksInstalled = HookTypes
		result.Success = true
		return result, nil
	}

	pluginDir := filepath.Dir(pluginFile)
	if err := os.MkdirAll(pluginDir, 0700); err != nil {
		result.Error = fmt.Errorf("failed to create plugins directory: %w", err)
		return result, result.Error
	}

	if err := os.WriteFile(pluginFile, pluginJS, 0644); err != nil {
		result.Error = fmt.Errorf("failed to write plugin file: %w", err)
		return result, result.Error
	}

	result.HooksInstalled = HookTypes
	result.Success = true
	return result, nil
}

func UninstallHooks(ctx context.Context, opts agent.UninstallOptions) (*agent.UninstallResult, error) {
	result := &agent.UninstallResult{}

	detection, err := Detect(ctx)
	if err != nil {
		result.Error = err
		return result, err
	}

	if !detection.Installed {
		result.Success = true
		return result, nil
	}

	pluginFile := pluginPath(detection.ConfigPath)

	if _, err := os.Stat(pluginFile); os.IsNotExist(err) {
		result.Success = true
		return result, nil
	}

	if opts.RestoreBackup && opts.BackupDir != "" {
		backupPath := filepath.Join(opts.BackupDir, "opencode", pluginFileName+".backup")
		if data, err := os.ReadFile(backupPath); err == nil {
			if !opts.DryRun {
				if err := os.WriteFile(pluginFile, data, 0644); err == nil {
					result.BackupsRestored = true
					result.HooksRemoved = HookTypes
					result.Success = true
					return result, nil
				}
			} else {
				result.HooksRemoved = HookTypes
				result.Success = true
				return result, nil
			}
		}
	}

	if opts.DryRun {
		result.HooksRemoved = HookTypes
		result.Success = true
		return result, nil
	}

	if err := os.Remove(pluginFile); err != nil {
		result.Error = fmt.Errorf("failed to remove plugin file: %w", err)
		return result, result.Error
	}

	result.HooksRemoved = HookTypes
	result.Success = true
	return result, nil
}

func GetHookStatus(ctx context.Context) (*agent.HookStatus, error) {
	status := &agent.HookStatus{}

	detection, err := Detect(ctx)
	if err != nil {
		return status, err
	}

	if !detection.Installed {
		return status, nil
	}

	pluginFile := pluginPath(detection.ConfigPath)
	data, err := os.ReadFile(pluginFile)
	if err != nil {
		if os.IsNotExist(err) {
			return status, nil
		}
		status.Issues = append(status.Issues, fmt.Sprintf("cannot read plugin file: %v", err))
		return status, nil
	}

	if !bytes.Contains(data, []byte("invokeGryph")) {
		status.Issues = append(status.Issues, "plugin file exists but does not contain gryph hooks")
		return status, nil
	}

	status.Installed = true
	status.Hooks = HookTypes
	status.Valid = bytes.Equal(data, pluginJS)
	if !status.Valid {
		status.Issues = append(status.Issues, "plugin file differs from expected content (may need update)")
	}

	return status, nil
}
