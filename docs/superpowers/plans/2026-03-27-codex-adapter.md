# Codex Agent Adapter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Gryph agent adapter for OpenAI Codex that captures session lifecycle, Bash tool usage, user prompt submission, and stop events via Codex's hooks system.

**Architecture:** Follows the existing adapter pattern — a `codex` package under `agent/` with adapter, detect, hooks, and parser files. Hook config uses Codex's matcher-based `hooks.json` at `~/.codex/hooks.json`. Integration wiring touches adapter constants, config, CLI registration, hook response handling, and TUI badge rendering.

**Tech Stack:** Go, Cobra CLI, Viper config, ent ORM (SQLite), testify, lipgloss TUI

---

## File Structure

| File | Responsibility |
|---|---|
| `agent/codex/adapter.go` | Adapter struct, interface methods, Register function |
| `agent/codex/detect.go` | Detection: check `~/.codex/` dir, run `codex --version` |
| `agent/codex/hooks.go` | Hook install/uninstall/status via matcher-based hooks.json |
| `agent/codex/parser.go` | Parse hook stdin JSON into events, build payloads, hook responses |
| `agent/codex/parser_test.go` | Unit tests for parser |
| `agent/codex/testdata/*.json` | JSON fixtures for each hook type |
| `agent/adapter.go` | Add constants `AgentCodex`, `DisplayCodex` |
| `agent/registry.go` | Add `"codex"` to `SupportedAgents()` |
| `config/config.go` | Add `agentNameCodex`, `Codex` field, config methods |
| `config/defaults.go` | Add default enabled setting |
| `config/validate.go` | Add logging level validation |
| `cli/root.go` | Import + Register call |
| `cli/hook.go` | Hook response + security blocked response handling |
| `tui/component/livelog/model.go` | Add to agentCycle |
| `tui/component/livelog/styles.go` | Add badge color |

---

### Task 1: Register Codex in Adapter Constants and Config

**Files:**
- Modify: `agent/adapter.go`
- Modify: `agent/registry.go`
- Modify: `config/config.go`
- Modify: `config/defaults.go`
- Modify: `config/validate.go`

- [ ] **Step 1: Add agent constants to `agent/adapter.go`**

In the agent identifiers block, add after the `AgentPiAgent` line:

```go
AgentCodex = "codex"
```

In the display names block, add after `DisplayPiAgent`:

```go
DisplayCodex = "Codex"
```

In the `AgentDisplayName` switch, add before `default`:

```go
case AgentCodex:
    return DisplayCodex
```

- [ ] **Step 2: Add to `SupportedAgents()` in `agent/registry.go`**

Add `"codex"` to the returned slice:

```go
func SupportedAgents() []string {
	return []string{
		"claude-code",
		"codex",
		"cursor",
		"gemini",
		"opencode",
		"openclaw",
		"windsurf",
		"pi-agent",
	}
}
```

- [ ] **Step 3: Add config support in `config/config.go`**

Add constant in the agent names block:

```go
agentNameCodex = "codex"
```

Add field to `AgentsConfig` struct after `PiAgent`:

```go
Codex AgentConfig `mapstructure:"codex"`
```

Add case in `GetAgentLoggingLevel()` after the `agentNamePiAgent` case:

```go
case agentNameCodex:
    if c.Agents.Codex.LoggingLevel != "" {
        return c.Agents.Codex.LoggingLevel
    }
```

Add case in `IsAgentEnabled()` after the `agentNamePiAgent` case:

```go
case agentNameCodex:
    return c.Agents.Codex.Enabled
```

- [ ] **Step 4: Add default in `config/defaults.go`**

Add after the `pi-agent` default:

```go
v.SetDefault("agents.codex.enabled", true)
```

- [ ] **Step 5: Add validation in `config/validate.go`**

Add after the `PiAgent` validation block:

```go
if cfg.Agents.Codex.LoggingLevel != "" && !isValidLoggingLevel(cfg.Agents.Codex.LoggingLevel) {
    return fmt.Errorf("invalid agents.codex.logging_level: %s", cfg.Agents.Codex.LoggingLevel)
}
```

- [ ] **Step 6: Run tests to verify config changes**

Run: `cd /workspace/gryph && go build ./...`
Expected: Build succeeds with no errors.

- [ ] **Step 7: Commit**

```bash
git add agent/adapter.go agent/registry.go config/config.go config/defaults.go config/validate.go
git commit -m "feat(codex): register Codex agent constants and config"
```

---

### Task 2: Create Codex Adapter and Detection

**Files:**
- Create: `agent/codex/adapter.go`
- Create: `agent/codex/detect.go`

- [ ] **Step 1: Create `agent/codex/adapter.go`**

```go
package codex

import (
	"context"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
)

const (
	AgentName   = agent.AgentCodex
	DisplayName = agent.DisplayCodex
)

var _ agent.Adapter = (*Adapter)(nil)

type Adapter struct {
	privacyChecker *events.PrivacyChecker
	loggingLevel   config.LoggingLevel
	contentHash    bool
}

func New(privacyChecker *events.PrivacyChecker, loggingLevel config.LoggingLevel, contentHash bool) *Adapter {
	return &Adapter{privacyChecker: privacyChecker, loggingLevel: loggingLevel, contentHash: contentHash}
}

func (a *Adapter) Name() string        { return AgentName }
func (a *Adapter) DisplayName() string  { return DisplayName }

func (a *Adapter) Detect(ctx context.Context) (*agent.DetectionResult, error) {
	return Detect(ctx)
}

func (a *Adapter) Install(ctx context.Context, opts agent.InstallOptions) (*agent.InstallResult, error) {
	return InstallHooks(ctx, opts)
}

func (a *Adapter) Uninstall(ctx context.Context, opts agent.UninstallOptions) (*agent.UninstallResult, error) {
	return UninstallHooks(ctx, opts)
}

func (a *Adapter) Status(ctx context.Context) (*agent.HookStatus, error) {
	return GetHookStatus(ctx)
}

func (a *Adapter) ParseEvent(ctx context.Context, hookType string, rawData []byte) (*events.Event, error) {
	return a.parseHookEvent(hookType, rawData)
}

func Register(registry *agent.Registry, privacyChecker *events.PrivacyChecker, loggingLevel config.LoggingLevel, contentHash bool) {
	registry.Register(New(privacyChecker, loggingLevel, contentHash))
}
```

- [ ] **Step 2: Create `agent/codex/detect.go`**

```go
package codex

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/safedep/gryph/agent"
)

func Detect(ctx context.Context) (*agent.DetectionResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return &agent.DetectionResult{
			Installed: false,
			Message:   "could not determine home directory",
		}, nil
	}

	codexDir := filepath.Join(home, ".codex")

	if _, err := os.Stat(codexDir); os.IsNotExist(err) {
		return &agent.DetectionResult{
			Installed: false,
			Message:   "Codex not installed (~/.codex not found)",
		}, nil
	}

	result := &agent.DetectionResult{
		Installed:  true,
		Path:       codexDir,
		ConfigPath: codexDir,
		HooksPath:  filepath.Join(codexDir, "hooks.json"),
	}

	result.Version = getVersion(ctx)

	return result, nil
}

func getVersion(ctx context.Context) string {
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	output, err := exec.CommandContext(cmdCtx, "codex", "--version").Output()
	if err != nil {
		return "unknown"
	}

	version := strings.TrimSpace(string(output))
	if parts := strings.Fields(version); len(parts) > 0 {
		for _, part := range parts {
			if len(part) > 0 && part[0] >= '0' && part[0] <= '9' {
				return part
			}
		}
	}

	return version
}
```

- [ ] **Step 3: Verify build compiles**

This will not compile yet because `parseHookEvent`, `InstallHooks`, `UninstallHooks`, and `GetHookStatus` are not yet defined. That is expected — we create stubs in the next tasks.

---

### Task 3: Create Codex Hook Management

**Files:**
- Create: `agent/codex/hooks.go`

- [ ] **Step 1: Create `agent/codex/hooks.go`**

```go
package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/utils"
)

var HookTypes = []string{
	"SessionStart",
	"PreToolUse",
	"PostToolUse",
	"UserPromptSubmit",
	"Stop",
}

type HooksConfig struct {
	Hooks map[string][]HookMatcher `json:"hooks"`
}

type HookMatcher struct {
	Matcher string        `json:"matcher"`
	Hooks   []HookCommand `json:"hooks"`
}

type HookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

func hookMatcher(hookType string) string {
	switch hookType {
	case "PreToolUse", "PostToolUse":
		return "Bash"
	default:
		return ""
	}
}

func GenerateHooksConfig() *HooksConfig {
	config := &HooksConfig{
		Hooks: make(map[string][]HookMatcher),
	}

	for _, hookType := range HookTypes {
		config.Hooks[hookType] = []HookMatcher{
			{
				Matcher: hookMatcher(hookType),
				Hooks: []HookCommand{
					{
						Type:    "command",
						Command: fmt.Sprintf("%s _hook codex %s", utils.GryphCommand(), hookType),
						Timeout: 30,
					},
				},
			},
		}
	}

	return config
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
		result.Error = fmt.Errorf("failed to detect Codex: %w", err)
		return result, result.Error
	}

	configDir := detection.ConfigPath
	hooksFile := detection.HooksPath

	if !opts.DryRun {
		if err := os.MkdirAll(configDir, 0700); err != nil {
			result.Error = fmt.Errorf("failed to create config directory: %w", err)
			return result, result.Error
		}
	}

	var existingConfig *HooksConfig
	if data, err := os.ReadFile(hooksFile); err == nil {
		existingConfig = &HooksConfig{}
		if err := json.Unmarshal(data, existingConfig); err != nil {
			result.Warnings = append(result.Warnings, "existing hooks.json is malformed, will be replaced")
			existingConfig = nil
		}
	}

	if existingConfig != nil {
		if !opts.Force && !opts.DryRun && hasGryphHooks(existingConfig) {
			result.Warnings = append(result.Warnings, "gryph hooks already installed (use --force to overwrite)")
			result.Success = true
			return result, nil
		}

		if opts.Backup && !opts.DryRun {
			var backupPath string
			if opts.BackupDir != "" {
				backupDir := filepath.Join(opts.BackupDir, "codex")
				if err := os.MkdirAll(backupDir, 0700); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("failed to create backup directory: %v", err))
				} else {
					backupPath = filepath.Join(backupDir, fmt.Sprintf("hooks.json.backup.%s", time.Now().Format("20060102150405")))
				}
			} else {
				backupPath = fmt.Sprintf("%s.backup.%s", hooksFile, time.Now().Format("20060102150405"))
			}
			if backupPath != "" {
				data, _ := json.MarshalIndent(existingConfig, "", "  ")
				if err := os.WriteFile(backupPath, data, 0600); err == nil {
					result.BackupPaths["hooks.json"] = backupPath
				} else {
					result.Warnings = append(result.Warnings, fmt.Sprintf("failed to backup hooks.json: %v", err))
				}
			}
		}

		if !opts.Force && !opts.DryRun {
			existingConfig = mergeHooksConfig(existingConfig)
		}
	}

	if opts.DryRun {
		result.HooksInstalled = HookTypes
		result.Success = true
		return result, nil
	}

	var newConfig *HooksConfig
	if existingConfig != nil && !opts.Force {
		newConfig = existingConfig
	} else {
		newConfig = GenerateHooksConfig()
	}

	data, err := json.MarshalIndent(newConfig, "", "  ")
	if err != nil {
		result.Error = fmt.Errorf("failed to marshal hooks config: %w", err)
		return result, result.Error
	}

	if err := os.WriteFile(hooksFile, data, 0600); err != nil {
		result.Error = fmt.Errorf("failed to write hooks.json: %w", err)
		return result, result.Error
	}

	result.HooksInstalled = HookTypes

	status, err := GetHookStatus(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("verification failed: %v", err))
	} else if !status.Valid {
		result.Warnings = append(result.Warnings, "hooks installed but validation failed")
		result.Warnings = append(result.Warnings, status.Issues...)
	}

	result.Success = true
	return result, nil
}

func mergeHooksConfig(existing *HooksConfig) *HooksConfig {
	gryphConfig := GenerateHooksConfig()

	for hookType, matchers := range gryphConfig.Hooks {
		found := false
		for _, m := range existing.Hooks[hookType] {
			for _, h := range m.Hooks {
				if h.Command == matchers[0].Hooks[0].Command {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			existing.Hooks[hookType] = append(existing.Hooks[hookType], matchers...)
		}
	}

	return existing
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

	hooksFile := detection.HooksPath

	data, err := os.ReadFile(hooksFile)
	if os.IsNotExist(err) {
		result.Success = true
		return result, nil
	} else if err != nil {
		result.Error = fmt.Errorf("failed to read hooks.json: %w", err)
		return result, result.Error
	}

	var config HooksConfig
	if err := json.Unmarshal(data, &config); err != nil {
		result.Error = fmt.Errorf("failed to parse hooks.json: %w", err)
		return result, result.Error
	}

	if opts.DryRun {
		result.HooksRemoved = HookTypes
		result.Success = true
		return result, nil
	}

	if opts.RestoreBackup && opts.BackupDir != "" {
		pattern := filepath.Join(opts.BackupDir, "codex", "hooks.json.backup.*")
		matches, _ := filepath.Glob(pattern)
		if len(matches) > 0 {
			backupPath := matches[len(matches)-1]
			if backupData, err := os.ReadFile(backupPath); err == nil {
				if err := os.WriteFile(hooksFile, backupData, 0600); err == nil {
					result.BackupsRestored = true
					result.HooksRemoved = HookTypes
					result.Success = true
					return result, nil
				}
			}
		}
	}

	for hookType := range config.Hooks {
		filtered := []HookMatcher{}
		for _, m := range config.Hooks[hookType] {
			filteredHooks := []HookCommand{}
			for _, h := range m.Hooks {
				if !utils.IsGryphCommand(h.Command) {
					filteredHooks = append(filteredHooks, h)
				}
			}
			if len(filteredHooks) > 0 {
				m.Hooks = filteredHooks
				filtered = append(filtered, m)
			} else {
				result.HooksRemoved = append(result.HooksRemoved, hookType)
			}
		}
		config.Hooks[hookType] = filtered
	}

	newData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		result.Error = fmt.Errorf("failed to marshal hooks config: %w", err)
		return result, result.Error
	}

	if err := os.WriteFile(hooksFile, newData, 0600); err != nil {
		result.Error = fmt.Errorf("failed to write hooks.json: %w", err)
		return result, result.Error
	}

	result.Success = true
	return result, nil
}

func hasGryphHooks(config *HooksConfig) bool {
	for _, hookType := range HookTypes {
		for _, m := range config.Hooks[hookType] {
			for _, h := range m.Hooks {
				if utils.IsGryphCommand(h.Command) {
					return true
				}
			}
		}
	}
	return false
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

	hooksFile := detection.HooksPath

	data, err := os.ReadFile(hooksFile)
	if os.IsNotExist(err) {
		return status, nil
	} else if err != nil {
		status.Issues = append(status.Issues, fmt.Sprintf("cannot read hooks.json: %v", err))
		return status, nil
	}

	var config HooksConfig
	if err := json.Unmarshal(data, &config); err != nil {
		status.Issues = append(status.Issues, "hooks.json is malformed")
		return status, nil
	}

	status.Valid = true

	for _, hookType := range HookTypes {
		expectedCmd := fmt.Sprintf("%s _hook codex %s", utils.GryphCommand(), hookType)
		for _, m := range config.Hooks[hookType] {
			for _, h := range m.Hooks {
				if h.Command == expectedCmd {
					status.Installed = true
					status.Hooks = append(status.Hooks, hookType)
					break
				}
			}
		}
	}

	if status.Installed {
		for _, hookType := range HookTypes {
			found := false
			for _, h := range status.Hooks {
				if h == hookType {
					found = true
					break
				}
			}
			if !found {
				status.Valid = false
				status.Issues = append(status.Issues, fmt.Sprintf("%s: hook not configured", hookType))
			}
		}
	}

	return status, nil
}
```

- [ ] **Step 2: Verify build compiles**

Run: `cd /workspace/gryph && go build ./agent/codex/...`
Expected: Fails because `parseHookEvent` is not yet defined. This is expected.

---

### Task 4: Create Test Fixtures

**Files:**
- Create: `agent/codex/testdata/session_start.json`
- Create: `agent/codex/testdata/pre_tool_use_bash.json`
- Create: `agent/codex/testdata/post_tool_use_bash.json`
- Create: `agent/codex/testdata/user_prompt_submit.json`
- Create: `agent/codex/testdata/stop.json`

- [ ] **Step 1: Create `agent/codex/testdata/session_start.json`**

```json
{
  "session_id": "codex-session-abc",
  "transcript_path": "/home/user/.codex/transcripts/test.jsonl",
  "cwd": "/home/user/project",
  "hook_event_name": "SessionStart",
  "model": "o4-mini",
  "source": "startup"
}
```

- [ ] **Step 2: Create `agent/codex/testdata/pre_tool_use_bash.json`**

```json
{
  "session_id": "codex-session-abc",
  "transcript_path": "/home/user/.codex/transcripts/test.jsonl",
  "cwd": "/home/user/project",
  "hook_event_name": "PreToolUse",
  "model": "o4-mini",
  "turn_id": "turn-001",
  "tool_name": "Bash",
  "tool_use_id": "tool-001",
  "tool_input": {
    "command": "npm install"
  }
}
```

- [ ] **Step 3: Create `agent/codex/testdata/post_tool_use_bash.json`**

```json
{
  "session_id": "codex-session-abc",
  "transcript_path": "/home/user/.codex/transcripts/test.jsonl",
  "cwd": "/home/user/project",
  "hook_event_name": "PostToolUse",
  "model": "o4-mini",
  "turn_id": "turn-001",
  "tool_name": "Bash",
  "tool_use_id": "tool-001",
  "tool_input": {
    "command": "npm install"
  },
  "tool_response": "added 150 packages in 12s"
}
```

- [ ] **Step 4: Create `agent/codex/testdata/user_prompt_submit.json`**

```json
{
  "session_id": "codex-session-abc",
  "transcript_path": "/home/user/.codex/transcripts/test.jsonl",
  "cwd": "/home/user/project",
  "hook_event_name": "UserPromptSubmit",
  "model": "o4-mini",
  "turn_id": "turn-002",
  "prompt": "Fix the failing test in main_test.go"
}
```

- [ ] **Step 5: Create `agent/codex/testdata/stop.json`**

```json
{
  "session_id": "codex-session-abc",
  "transcript_path": "/home/user/.codex/transcripts/test.jsonl",
  "cwd": "/home/user/project",
  "hook_event_name": "Stop",
  "model": "o4-mini",
  "turn_id": "turn-003",
  "stop_hook_active": true,
  "last_assistant_message": "All tests are now passing."
}
```

- [ ] **Step 6: Commit fixtures**

```bash
git add agent/codex/testdata/
git commit -m "feat(codex): add test fixtures for all hook types"
```

---

### Task 5: Create Parser with Tests (TDD)

**Files:**
- Create: `agent/codex/parser.go`
- Create: `agent/codex/parser_test.go`

- [ ] **Step 1: Write parser tests in `agent/codex/parser_test.go`**

```go
package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "Failed to read fixture: %s", name)
	return data
}

func testPrivacyChecker(t *testing.T) *events.PrivacyChecker {
	t.Helper()
	pc, err := events.NewPrivacyChecker(events.DefaultSensitivePatterns(), nil)
	require.NoError(t, err)
	return pc
}

func testAdapter(t *testing.T) *Adapter {
	t.Helper()
	return New(testPrivacyChecker(t), config.LoggingStandard, true)
}

func TestParseHookEvent_SessionStart(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "session_start.json")

	event, err := testAdapter(t).ParseEvent(ctx, "SessionStart", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionSessionStart, event.ActionType)
	assert.Equal(t, AgentName, event.AgentName)
	assert.Equal(t, "codex-session-abc", event.AgentSessionID)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)

	payload := events.SessionPayload{}
	require.NoError(t, json.Unmarshal(event.Payload, &payload))
	assert.Equal(t, "startup", payload.Source)
	assert.Equal(t, "o4-mini", payload.Model)
}

func TestParseHookEvent_PreToolUse_Bash(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_tool_use_bash.json")

	event, err := testAdapter(t).ParseEvent(ctx, "PreToolUse", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionCommandExec, event.ActionType)
	assert.Equal(t, "Bash", event.ToolName)
	assert.Equal(t, AgentName, event.AgentName)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)

	payload, err := event.GetCommandExecPayload()
	require.NoError(t, err)
	assert.Equal(t, "npm install", payload.Command)
}

func TestParseHookEvent_PostToolUse_Bash(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "post_tool_use_bash.json")

	event, err := testAdapter(t).ParseEvent(ctx, "PostToolUse", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionCommandExec, event.ActionType)
	assert.Equal(t, "Bash", event.ToolName)
	assert.Equal(t, events.ResultSuccess, event.ResultStatus)

	payload, err := event.GetCommandExecPayload()
	require.NoError(t, err)
	assert.Equal(t, "npm install", payload.Command)
	assert.Contains(t, payload.Output, "added 150 packages")
}

func TestParseHookEvent_UserPromptSubmit(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "user_prompt_submit.json")

	event, err := testAdapter(t).ParseEvent(ctx, "UserPromptSubmit", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionToolUse, event.ActionType)
	assert.Equal(t, "UserPromptSubmit", event.ToolName)

	payload, err := event.GetToolUsePayload()
	require.NoError(t, err)
	assert.Equal(t, "UserPromptSubmit", payload.ToolName)
}

func TestParseHookEvent_Stop(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "stop.json")

	event, err := testAdapter(t).ParseEvent(ctx, "Stop", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionSessionEnd, event.ActionType)

	payload := events.SessionEndPayload{}
	require.NoError(t, json.Unmarshal(event.Payload, &payload))
	assert.Equal(t, "All tests are now passing.", payload.Reason)
}

func TestParseHookEvent_DeterministicSessionID(t *testing.T) {
	ctx := context.Background()
	adapter := testAdapter(t)

	data1 := loadFixture(t, "pre_tool_use_bash.json")
	data2 := loadFixture(t, "post_tool_use_bash.json")

	event1, err := adapter.ParseEvent(ctx, "PreToolUse", data1)
	require.NoError(t, err)

	event2, err := adapter.ParseEvent(ctx, "PostToolUse", data2)
	require.NoError(t, err)

	assert.Equal(t, event1.SessionID, event2.SessionID)

	expected := uuid.NewSHA1(uuid.NameSpaceOID, []byte("codex-session-abc"))
	assert.Equal(t, expected, event1.SessionID)
}

func TestParseHookEvent_InvalidJSON(t *testing.T) {
	ctx := context.Background()
	adapter := testAdapter(t)

	_, err := adapter.ParseEvent(ctx, "SessionStart", []byte("not-json"))
	assert.Error(t, err)
}

func TestHookResponse_Allow(t *testing.T) {
	resp := NewAllowResponse()
	data := resp.JSON()

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))

	hookOutput, ok := parsed["hookSpecificOutput"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "approve", hookOutput["permissionDecision"])
}

func TestHookResponse_Block(t *testing.T) {
	resp := NewBlockResponse("dangerous command")
	data := resp.JSON()

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))

	hookOutput, ok := parsed["hookSpecificOutput"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "deny", hookOutput["permissionDecision"])
	assert.Equal(t, "dangerous command", hookOutput["permissionDecisionReason"])
}

func TestHookResponse_ExitCodes(t *testing.T) {
	assert.Equal(t, 0, NewAllowResponse().ExitCode())
	assert.Equal(t, 0, NewBlockResponse("reason").ExitCode())
	assert.Equal(t, 2, NewErrorResponse("error").ExitCode())
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /workspace/gryph && go test ./agent/codex/...`
Expected: Compilation error — `parseHookEvent` and response types not defined.

- [ ] **Step 3: Create `agent/codex/parser.go`**

```go
package codex

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
)

type HookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	Model          string `json:"model"`
}

type PreToolUseInput struct {
	HookInput
	TurnID    string                 `json:"turn_id"`
	ToolName  string                 `json:"tool_name"`
	ToolUseID string                 `json:"tool_use_id"`
	ToolInput map[string]interface{} `json:"tool_input"`
}

type PostToolUseInput struct {
	HookInput
	TurnID       string                 `json:"turn_id"`
	ToolName     string                 `json:"tool_name"`
	ToolUseID    string                 `json:"tool_use_id"`
	ToolInput    map[string]interface{} `json:"tool_input"`
	ToolResponse json.RawMessage        `json:"tool_response"`
}

type SessionStartInput struct {
	HookInput
	Source string `json:"source"`
}

type UserPromptSubmitInput struct {
	HookInput
	TurnID string `json:"turn_id"`
	Prompt string `json:"prompt"`
}

type StopInput struct {
	HookInput
	TurnID              string `json:"turn_id"`
	StopHookActive      bool   `json:"stop_hook_active"`
	LastAssistantMessage string `json:"last_assistant_message"`
}

var ToolNameMapping = map[string]events.ActionType{
	"Bash": events.ActionCommandExec,
}

func (a *Adapter) parseHookEvent(hookType string, rawData []byte) (*events.Event, error) {
	var baseInput HookInput
	if err := json.Unmarshal(rawData, &baseInput); err != nil {
		return nil, fmt.Errorf("failed to parse hook input: %w", err)
	}

	eventName := hookType
	if eventName == "" {
		eventName = baseInput.HookEventName
	}

	sessionID := resolveSessionID(baseInput.SessionID)
	agentSessionID := baseInput.SessionID

	switch eventName {
	case "SessionStart":
		return parseSessionStart(sessionID, agentSessionID, baseInput, rawData)
	case "PreToolUse":
		return a.parsePreToolUse(sessionID, agentSessionID, baseInput, rawData)
	case "PostToolUse":
		return a.parsePostToolUse(sessionID, agentSessionID, baseInput, rawData)
	case "UserPromptSubmit":
		return parseUserPromptSubmit(sessionID, agentSessionID, baseInput, rawData)
	case "Stop":
		return parseStop(sessionID, agentSessionID, baseInput, rawData)
	default:
		event := events.NewEvent(sessionID, AgentName, events.ActionUnknown)
		event.AgentSessionID = agentSessionID
		event.WorkingDirectory = baseInput.Cwd
		event.TranscriptPath = baseInput.TranscriptPath
		event.RawEvent = rawData
		return event, nil
	}
}

func resolveSessionID(rawSessionID string) uuid.UUID {
	if envID := os.Getenv("CODEX_SESSION_ID"); envID != "" {
		rawSessionID = envID
	}

	if rawSessionID != "" {
		if parsed, err := uuid.Parse(rawSessionID); err == nil {
			return parsed
		}
		return uuid.NewSHA1(uuid.NameSpaceOID, []byte(rawSessionID))
	}

	return uuid.New()
}

func parseSessionStart(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input SessionStartInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse SessionStart input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionSessionStart)
	event.AgentSessionID = agentSessionID
	event.WorkingDirectory = input.Cwd
	event.TranscriptPath = input.TranscriptPath
	event.RawEvent = rawData

	payload := events.SessionPayload{
		Source: input.Source,
		Model:  input.Model,
	}

	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func (a *Adapter) parsePreToolUse(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input PreToolUseInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse PreToolUse input: %w", err)
	}

	actionType := getActionType(input.ToolName)
	event := events.NewEvent(sessionID, AgentName, actionType)
	event.AgentSessionID = agentSessionID
	event.ToolName = input.ToolName
	event.WorkingDirectory = input.Cwd
	event.TranscriptPath = input.TranscriptPath
	event.RawEvent = rawData

	if err := buildToolPayload(event, actionType, input.ToolInput, nil); err != nil {
		return nil, fmt.Errorf("failed to build payload: %w", err)
	}

	a.markSensitivePaths(event, actionType, input.ToolInput)

	return event, nil
}

func (a *Adapter) parsePostToolUse(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input PostToolUseInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse PostToolUse input: %w", err)
	}

	actionType := getActionType(input.ToolName)
	event := events.NewEvent(sessionID, AgentName, actionType)
	event.AgentSessionID = agentSessionID
	event.ToolName = input.ToolName
	event.WorkingDirectory = input.Cwd
	event.TranscriptPath = input.TranscriptPath
	event.RawEvent = rawData
	event.ResultStatus = events.ResultSuccess

	var responseStr string
	if err := json.Unmarshal(input.ToolResponse, &responseStr); err == nil {
		if err := buildToolPayload(event, actionType, input.ToolInput, responseStr); err != nil {
			return nil, fmt.Errorf("failed to build payload: %w", err)
		}
	} else {
		if err := buildToolPayload(event, actionType, input.ToolInput, nil); err != nil {
			return nil, fmt.Errorf("failed to build payload: %w", err)
		}
	}

	a.markSensitivePaths(event, actionType, input.ToolInput)

	return event, nil
}

func parseUserPromptSubmit(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input UserPromptSubmitInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse UserPromptSubmit input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionToolUse)
	event.AgentSessionID = agentSessionID
	event.ToolName = "UserPromptSubmit"
	event.WorkingDirectory = input.Cwd
	event.TranscriptPath = input.TranscriptPath
	event.RawEvent = rawData

	payload := events.ToolUsePayload{
		ToolName: "UserPromptSubmit",
	}

	promptInput := map[string]string{"prompt": input.Prompt}
	if data, err := json.Marshal(promptInput); err == nil {
		payload.Input = data
	}

	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func parseStop(sessionID uuid.UUID, agentSessionID string, base HookInput, rawData []byte) (*events.Event, error) {
	var input StopInput
	if err := json.Unmarshal(rawData, &input); err != nil {
		return nil, fmt.Errorf("failed to parse Stop input: %w", err)
	}

	event := events.NewEvent(sessionID, AgentName, events.ActionSessionEnd)
	event.AgentSessionID = agentSessionID
	event.WorkingDirectory = input.Cwd
	event.TranscriptPath = input.TranscriptPath
	event.RawEvent = rawData

	payload := events.SessionEndPayload{
		Reason: input.LastAssistantMessage,
	}

	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return event, nil
}

func getActionType(toolName string) events.ActionType {
	if at, ok := ToolNameMapping[toolName]; ok {
		return at
	}
	return events.ActionToolUse
}

func buildToolPayload(event *events.Event, actionType events.ActionType, toolInput map[string]interface{}, toolResponse interface{}) error {
	switch actionType {
	case events.ActionCommandExec:
		payload := events.CommandExecPayload{}
		if cmd, ok := toolInput["command"].(string); ok {
			payload.Command = cmd
		}
		if responseStr, ok := toolResponse.(string); ok {
			payload.Output = truncateString(responseStr, 500)
		}
		return event.SetPayload(payload)

	default:
		payload := events.ToolUsePayload{
			ToolName: event.ToolName,
		}
		if input, err := json.Marshal(toolInput); err == nil {
			payload.Input = input
		}
		if toolResponse != nil {
			if resp, err := json.Marshal(toolResponse); err == nil {
				payload.Output = resp
			}
		}
		return event.SetPayload(payload)
	}
}

func (a *Adapter) markSensitivePaths(event *events.Event, actionType events.ActionType, toolInput map[string]interface{}) {
	if a.privacyChecker == nil {
		return
	}

	if actionType == events.ActionCommandExec {
		if cmd, ok := toolInput["command"].(string); ok {
			event.IsSensitive = a.privacyChecker.IsSensitivePath(cmd)
		}
	}
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Hook response types for Codex.
// Codex PreToolUse uses JSON on stdout with permissionDecision.
// Exit 0 for allow and block; exit 2 for error.

type HookResponse struct {
	Decision HookDecision
	Message  string
}

type HookDecision int

const (
	HookAllow HookDecision = iota
	HookBlock
	HookError
)

func (r *HookResponse) ExitCode() int {
	if r.Decision == HookError {
		return 2
	}
	return 0
}

type preToolUseOutput struct {
	HookSpecificOutput preToolUseDecision `json:"hookSpecificOutput"`
}

type preToolUseDecision struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
}

func (r *HookResponse) JSON() []byte {
	output := preToolUseOutput{
		HookSpecificOutput: preToolUseDecision{
			HookEventName: "PreToolUse",
		},
	}

	switch r.Decision {
	case HookBlock:
		output.HookSpecificOutput.PermissionDecision = "deny"
		output.HookSpecificOutput.PermissionDecisionReason = r.Message
	default:
		output.HookSpecificOutput.PermissionDecision = "approve"
	}

	data, _ := json.Marshal(output)
	return data
}

func NewAllowResponse() *HookResponse {
	return &HookResponse{Decision: HookAllow}
}

func NewBlockResponse(message string) *HookResponse {
	return &HookResponse{Decision: HookBlock, Message: message}
}

func NewErrorResponse(message string) *HookResponse {
	return &HookResponse{Decision: HookError, Message: message}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /workspace/gryph && go test ./agent/codex/ -v`
Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add agent/codex/
git commit -m "feat(codex): add adapter, detection, hooks, and parser with tests"
```

---

### Task 6: Wire Up CLI Registration and Hook Responses

**Files:**
- Modify: `cli/root.go`
- Modify: `cli/hook.go`

- [ ] **Step 1: Add import and Register call in `cli/root.go`**

Add import:

```go
"github.com/safedep/gryph/agent/codex"
```

Add registration after the `piagent.Register(...)` line (before the openclaw comment):

```go
codex.Register(registry, privacyChecker, cfg.GetAgentLoggingLevel(agent.AgentCodex), cfg.Logging.ContentHash)
```

- [ ] **Step 2: Add import in `cli/hook.go`**

Add to imports:

```go
"github.com/safedep/gryph/agent/codex"
```

- [ ] **Step 3: Add Codex case in `sendHookResponse()`**

Add before the `default` case:

```go
case agent.AgentCodex:
	// Codex: PreToolUse uses JSON response on stdout with permissionDecision.
	// Other hooks: exit 0 with no output.
	if hookType == "PreToolUse" {
		resp := codex.NewAllowResponse()
		if _, err := os.Stdout.Write(resp.JSON()); err != nil {
			log.Errorf("failed to write to stdout: %v", err)
		}
	}
	return nil
```

- [ ] **Step 4: Add Codex case in `sendSecurityBlockedResponse()`**

Add before the `default` case:

```go
case agent.AgentCodex:
	if hookType == "PreToolUse" {
		response := codex.NewBlockResponse(result.BlockReason)
		if _, err := os.Stdout.Write(response.JSON()); err != nil {
			log.Errorf("failed to write to stdout: %v", err)
		}
		return nil
	}
	response := codex.NewErrorResponse(result.BlockReason)
	return handleCodexResponse(response)
```

- [ ] **Step 5: Add `handleCodexResponse()` function**

Add after the `handlePiAgentResponse` function:

```go
func handleCodexResponse(response *codex.HookResponse) error {
	switch response.Decision {
	case codex.HookError:
		return &exitError{code: 2, message: response.Message}
	default:
		return nil
	}
}
```

- [ ] **Step 6: Verify build**

Run: `cd /workspace/gryph && go build ./...`
Expected: Build succeeds.

- [ ] **Step 7: Commit**

```bash
git add cli/root.go cli/hook.go
git commit -m "feat(codex): wire up CLI registration and hook response handling"
```

---

### Task 7: Add TUI Badge

**Files:**
- Modify: `tui/component/livelog/model.go`
- Modify: `tui/component/livelog/styles.go`

- [ ] **Step 1: Add `"codex"` to `agentCycle` in `model.go`**

Update the slice to include `"codex"`:

```go
var agentCycle = []string{"", "claude-code", "codex", "cursor", "gemini", "opencode", "openclaw", "windsurf", "pi-agent"}
```

- [ ] **Step 2: Add yellow color and badge case in `styles.go`**

Add `colorYellow` to the color variables:

```go
colorYellow = lipgloss.Color("#F1C40F")
```

Add case in `agentBadge()` before the `default`:

```go
case "codex":
    return lipgloss.NewStyle().Foreground(colorYellow).Bold(true).Render("codex")
```

- [ ] **Step 3: Verify build**

Run: `cd /workspace/gryph && go build ./...`
Expected: Build succeeds.

- [ ] **Step 4: Commit**

```bash
git add tui/component/livelog/model.go tui/component/livelog/styles.go
git commit -m "feat(codex): add TUI badge with yellow color"
```

---

### Task 8: Add E2E Hook Tests

**Files:**
- Modify: `test/cli/e2e_hook_test.go`

- [ ] **Step 1: Add `TestHook_Codex` to `test/cli/e2e_hook_test.go`**

Add at the end of the file:

```go
func TestHook_Codex(t *testing.T) {
	tests := []struct {
		name       string
		hookType   string
		fixture    string
		actionType events.ActionType
	}{
		{
			name:       "PreToolUse_Bash",
			hookType:   "PreToolUse",
			fixture:    "pre_tool_use_bash.json",
			actionType: events.ActionCommandExec,
		},
		{
			name:       "PostToolUse_Bash",
			hookType:   "PostToolUse",
			fixture:    "post_tool_use_bash.json",
			actionType: events.ActionCommandExec,
		},
		{
			name:       "SessionStart",
			hookType:   "SessionStart",
			fixture:    "session_start.json",
			actionType: events.ActionSessionStart,
		},
		{
			name:       "UserPromptSubmit",
			hookType:   "UserPromptSubmit",
			fixture:    "user_prompt_submit.json",
			actionType: events.ActionToolUse,
		},
		{
			name:       "Stop",
			hookType:   "Stop",
			fixture:    "stop.json",
			actionType: events.ActionSessionEnd,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			ctx := context.Background()

			payload, err := os.ReadFile("../../agent/codex/testdata/" + tt.fixture)
			require.NoError(t, err)

			_, _, runErr := env.runHook("codex", tt.hookType, payload)
			require.NoError(t, runErr)

			store, cleanup := env.openStore()
			defer cleanup()

			evts, err := store.QueryEvents(ctx, events.NewEventFilter())
			require.NoError(t, err)
			require.Len(t, evts, 1)

			assert.Equal(t, tt.actionType, evts[0].ActionType)
			assert.Equal(t, "codex", evts[0].AgentName)
		})
	}
}

func TestHook_Codex_DeterministicSessionID(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	payload1, err := os.ReadFile("../../agent/codex/testdata/pre_tool_use_bash.json")
	require.NoError(t, err)

	payload2, err := os.ReadFile("../../agent/codex/testdata/post_tool_use_bash.json")
	require.NoError(t, err)

	_, _, err = env.runHook("codex", "PreToolUse", payload1)
	require.NoError(t, err)

	_, _, err = env.runHook("codex", "PostToolUse", payload2)
	require.NoError(t, err)

	store, cleanup := env.openStore()
	defer cleanup()

	evts, err := store.QueryEvents(ctx, events.NewEventFilter())
	require.NoError(t, err)
	require.Len(t, evts, 2)

	assert.Equal(t, evts[0].SessionID, evts[1].SessionID, "same session_id should produce same UUID")
}
```

- [ ] **Step 2: Run E2E tests**

Run: `cd /workspace/gryph && go test ./test/cli/ -run TestHook_Codex -v`
Expected: All tests pass.

- [ ] **Step 3: Commit**

```bash
git add test/cli/e2e_hook_test.go
git commit -m "test(codex): add E2E hook tests for all hook types"
```

---

### Task 9: Run Full Test Suite and Lint

- [ ] **Step 1: Run all tests**

Run: `cd /workspace/gryph && make test`
Expected: All tests pass.

- [ ] **Step 2: Run linter**

Run: `cd /workspace/gryph && make lint`
Expected: No lint errors.

- [ ] **Step 3: Build binary and verify dry-run**

Run: `cd /workspace/gryph && make gryph && ./bin/gryph install --agent codex --dry-run`
Expected: Shows the hooks that would be installed for Codex.

- [ ] **Step 4: Fix any issues found, commit if needed**
