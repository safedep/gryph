package piagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/safedep/gryph/agent"
)

var HookTypes = []string{
	"tool_call",
	"tool_result",
	"session_start",
	"session_shutdown",
}

const gryphExtensionContent = `import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { spawn } from "node:child_process";

export default function (pi: ExtensionAPI) {
  pi.on("session_start", async (event, ctx) => {
    sendToGryph("session_start", {
      session_id: ctx.sessionManager.getSessionFile() ?? "ephemeral",
      cwd: ctx.cwd,
    });
  });

  pi.on("session_shutdown", async (event, ctx) => {
    sendToGryph("session_shutdown", {
      session_id: ctx.sessionManager.getSessionFile() ?? "ephemeral",
      cwd: ctx.cwd,
    });
  });

  pi.on("tool_call", async (event, ctx) => {
    sendToGryph("tool_call", {
      session_id: ctx.sessionManager.getSessionFile() ?? "ephemeral",
      cwd: ctx.cwd,
      tool_name: event.toolName,
      tool_call_id: event.toolCallId,
      input: event.input,
    });
  });

  pi.on("tool_result", async (event, ctx) => {
    sendToGryph("tool_result", {
      session_id: ctx.sessionManager.getSessionFile() ?? "ephemeral",
      cwd: ctx.cwd,
      tool_name: event.toolName,
      tool_call_id: event.toolCallId,
      input: event.input,
      content: event.content,
      is_error: event.isError,
    });
  });
}

function sendToGryph(hookType: string, data: Record<string, unknown>) {
  const payload = JSON.stringify({
    hook_event_name: hookType,
    ...data,
    timestamp: new Date().toISOString(),
  });

  const child = spawn("gryph", ["_hook", "pi-agent", hookType], {
    stdio: ["pipe", "pipe", "pipe"],
  });

  child.stdin.write(payload);
  child.stdin.end();

  // Fire-and-forget: silently ignore errors
}
`

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
		result.Error = fmt.Errorf("failed to detect pi agent: %w", err)
		return result, result.Error
	}

	extensionsDir := filepath.Join(detection.ConfigPath, "extensions")
	if err := os.MkdirAll(extensionsDir, 0755); err != nil {
		result.Error = fmt.Errorf("failed to create extensions directory: %w", err)
		return result, result.Error
	}

	extensionPath := filepath.Join(extensionsDir, "gryph-hooks.ts")

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
					backupPath = filepath.Join(backupDir, fmt.Sprintf("gryph-hooks.ts.backup.%s", time.Now().Format("20060102150405")))
				}
			} else {
				backupPath = fmt.Sprintf("%s.backup.%s", extensionPath, time.Now().Format("20060102150405"))
			}

			if backupPath != "" {
				if data, err := os.ReadFile(extensionPath); err == nil {
					if err := os.WriteFile(backupPath, data, 0600); err == nil {
						result.BackupPaths["gryph-hooks.ts"] = backupPath
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

	if err := os.WriteFile(extensionPath, []byte(gryphExtensionContent), 0644); err != nil {
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

	extensionPath := filepath.Join(detection.ConfigPath, "extensions", "gryph-hooks.ts")

	if opts.RestoreBackup && opts.BackupDir != "" {
		pattern := filepath.Join(opts.BackupDir, "pi-agent", "gryph-hooks.ts.backup.*")
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

	extensionPath := filepath.Join(detection.ConfigPath, "extensions", "gryph-hooks.ts")

	data, err := os.ReadFile(extensionPath)
	if os.IsNotExist(err) {
		return status, nil
	}
	if err != nil {
		status.Issues = append(status.Issues, fmt.Sprintf("cannot read extension: %v", err))
		return status, nil
	}

	expectedContent := gryphExtensionContent
	if string(data) == expectedContent {
		status.Installed = true
		status.Valid = true
		status.Hooks = HookTypes
	} else {
		status.Installed = true
		status.Valid = true
		status.Hooks = HookTypes
		status.Issues = append(status.Issues, "extension content differs from expected (may have been modified)")
	}

	return status, nil
}
