package piagent

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/utils"
)

//go:embed plugin.ts
var pluginTS []byte

func processedPlugin() []byte {
	return bytes.ReplaceAll(pluginTS, []byte(utils.GryphCommandPlaceholder), []byte(utils.GryphCommand()))
}

var HookTypes = []string{
	"tool_call",
	"tool_result",
	"session_start",
	"session_shutdown",
}

const hookFileName = "gryph-hooks.ts"

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
		result.Error = fmt.Errorf("pi agent not detected (~/.pi/agent not found)")
		return result, result.Error
	}

	extensionsDir := filepath.Join(detection.ConfigPath, "extensions")
	if err := os.MkdirAll(extensionsDir, 0755); err != nil {
		result.Error = fmt.Errorf("failed to create extensions directory: %w", err)
		return result, result.Error
	}

	extensionPath := filepath.Join(extensionsDir, hookFileName)

	if _, err := os.Stat(extensionPath); err == nil && !opts.Force && !opts.DryRun {
		result.Warnings = append(result.Warnings, "gryph hooks already installed (use --force to overwrite)")
		result.Success = true
		return result, nil
	}

	if opts.Backup && !opts.DryRun {
		if _, err := os.Stat(extensionPath); err == nil {
			var backupPath string
			if opts.BackupDir != "" {
				backupDir := filepath.Join(opts.BackupDir, "pi-agent")
				if err := os.MkdirAll(backupDir, 0700); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("failed to create backup directory: %v", err))
				} else {
					backupPath = filepath.Join(backupDir, fmt.Sprintf("%s.backup.%s", hookFileName, time.Now().Format("20060102150405")))
				}
			} else {
				backupPath = fmt.Sprintf("%s.backup.%s", extensionPath, time.Now().Format("20060102150405"))
			}

			if backupPath != "" {
				if data, err := os.ReadFile(extensionPath); err == nil {
					if err := os.WriteFile(backupPath, data, 0600); err == nil {
						result.BackupPaths[hookFileName] = backupPath
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

	if err := os.WriteFile(extensionPath, processedPlugin(), 0644); err != nil {
		result.Error = fmt.Errorf("failed to write extension: %w", err)
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

	extensionPath := filepath.Join(detection.ConfigPath, "extensions", hookFileName)

	if opts.RestoreBackup && opts.BackupDir != "" {
		pattern := filepath.Join(opts.BackupDir, "pi-agent", fmt.Sprintf("%s.backup.*", hookFileName))
		matches, _ := filepath.Glob(pattern)
		if len(matches) > 0 {
			backupPath := matches[len(matches)-1]
			if data, err := os.ReadFile(backupPath); err == nil {
				if !opts.DryRun {
					if err := os.WriteFile(extensionPath, data, 0644); err == nil {
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
	}

	if _, err := os.Stat(extensionPath); os.IsNotExist(err) {
		result.Success = true
		return result, nil
	}

	if opts.DryRun {
		result.HooksRemoved = HookTypes
		result.Success = true
		return result, nil
	}

	if err := os.Remove(extensionPath); err != nil {
		result.Error = fmt.Errorf("failed to remove extension: %w", err)
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

	extensionPath := filepath.Join(detection.ConfigPath, "extensions", hookFileName)

	data, err := os.ReadFile(extensionPath)
	if os.IsNotExist(err) {
		return status, nil
	}
	if err != nil {
		status.Issues = append(status.Issues, fmt.Sprintf("cannot read extension: %v", err))
		return status, nil
	}

	expectedContent := string(processedPlugin())
	if string(data) == expectedContent {
		status.Installed = true
		status.Valid = true
		status.Hooks = HookTypes
	} else {
		status.Installed = true
		status.Valid = false
		status.Hooks = HookTypes
		status.Issues = append(status.Issues, "extension content differs from expected (may have been modified)")
	}

	return status, nil
}
