// Package config provides configuration management using Viper.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/safedep/dry/log"
	"github.com/spf13/viper"
)

// Agent names as constants. Must be in sync with agent/adapter.go.
// We cannot depend on agent/adapter.go because it would create a circular dependency.
const (
	agentNameClaudeCode = "claude-code"
	agentNameCursor     = "cursor"
	agentNameGemini     = "gemini"
	agentNameOpenCode   = "opencode"
	agentNameOpenClaw   = "openclaw"
	agentNameWindsurf   = "windsurf"
	agentNamePiAgent    = "pi-agent"
	agentNameCodex      = "codex"

	streamTargetTypeStdout = "stdout"
	streamTargetTypeNop    = "nop"
)

// LoggingLevel represents the verbosity level for logging.
// This is for agent event logging only. Not for our own internal logging.
type LoggingLevel string

const (
	// LoggingMinimal logs action type, file path, timestamp, result only.
	LoggingMinimal LoggingLevel = "minimal"
	// LoggingStandard adds diff stats, command exit codes, truncated output.
	LoggingStandard LoggingLevel = "standard"
	// LoggingFull adds raw events, conversation context, full command output, file diffs.
	LoggingFull LoggingLevel = "full"
)

// loggingLevelOrder maps each level to a numeric value for comparison.
var loggingLevelOrder = map[LoggingLevel]int{
	LoggingMinimal:  0,
	LoggingStandard: 1,
	LoggingFull:     2,
}

// IsAtLeast returns true if this logging level is at least as verbose as other.
func (l LoggingLevel) IsAtLeast(other LoggingLevel) bool {
	return loggingLevelOrder[l] >= loggingLevelOrder[other]
}

// ColorMode represents the color output mode.
type ColorMode string

const (
	// ColorAuto automatically detects terminal support.
	ColorAuto ColorMode = "auto"
	// ColorAlways always uses colors.
	ColorAlways ColorMode = "always"
	// ColorNever never uses colors.
	ColorNever ColorMode = "never"
)

// TimezoneMode represents the timezone display mode.
type TimezoneMode string

const (
	// TimezoneLocal uses the local timezone.
	TimezoneLocal TimezoneMode = "local"
	// TimezoneUTC uses UTC.
	TimezoneUTC TimezoneMode = "utc"
)

// Config holds all configuration values.
type Config struct {
	Logging LoggingConfig `mapstructure:"logging"`
	Storage StorageConfig `mapstructure:"storage"`
	Privacy PrivacyConfig `mapstructure:"privacy"`
	Filters FiltersConfig `mapstructure:"filters"`
	Agents  AgentsConfig  `mapstructure:"agents"`
	Display DisplayConfig `mapstructure:"display"`
	Streams StreamsConfig `mapstructure:"streams"`
	Policy  PolicyConfig  `mapstructure:"policy"`
}

// PolicyConfig holds Gryph policy-layer settings.
type PolicyConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	FailMode string `mapstructure:"fail_mode"`

	// PolicyPaths is an ordered list of files or directories. Files are loaded
	// as-is; directories are scanned non-recursively for *.yaml / *.yml.
	PolicyPaths []string `mapstructure:"policy_paths"`
	// ConventionalPaths enables walking up from the current working directory
	// to discover a project-local .gryph-policy.yml / .gryph-policy.yaml.
	ConventionalPaths bool `mapstructure:"conventional_paths"`

	ContextRetentionDays int  `mapstructure:"context_retention_days"`
	ReceiptRetentionDays int  `mapstructure:"receipt_retention_days"`
	LogAllEvaluations    bool `mapstructure:"log_all_evaluations"`

	Approval       ApprovalConfig       `mapstructure:"approval"`
	Classify       ClassifyConfig       `mapstructure:"classify"`
	InjectionScore InjectionScoreConfig `mapstructure:"injection_score"`
	Receipts       ReceiptsConfig       `mapstructure:"receipts"`
}

// ReceiptsConfig configures cryptographic signing for AARM receipts. SignMode
// defaults to "auto": Gryph signs when a key is present and silently runs
// unsigned when no key exists. Set to "always" to hard-fail on a missing
// key, or "never" to disable signing entirely. The legacy bool `sign` is a
// deprecated alias mapped to `always` (true) or `never` (false).
type ReceiptsConfig struct {
	Sign       bool   `mapstructure:"sign"`
	SignMode   string `mapstructure:"sign_mode"`
	KeyPath    string `mapstructure:"key_path"`
	TrustStore string `mapstructure:"trust_store"`
}

// Sign mode constants.
const (
	SignModeAuto   = "auto"
	SignModeAlways = "always"
	SignModeNever  = "never"
)

var signDeprecationOnce sync.Once

// EffectiveSignMode returns the configured sign_mode. After Load() it is
// always the resolved value. The legacy bool is normalized into SignMode
// during Load(), and SignMode itself is trimmed and lowercased there. When
// called on a hand-constructed config that bypassed Load() and left
// SignMode empty, the auto default is returned.
func (r ReceiptsConfig) EffectiveSignMode() string {
	if r.SignMode == "" {
		return SignModeAuto
	}
	return r.SignMode
}

// ApprovalMode names the configured approval frontend.
type ApprovalMode string

const (
	// ApprovalModeNop denies every escalated action without prompting.
	ApprovalModeNop ApprovalMode = "nop"
	// ApprovalModeCLI prompts the operator interactively via /dev/tty.
	ApprovalModeCLI ApprovalMode = "cli"
)

// ApprovalConfig configures the approval workflow for escalated decisions.
type ApprovalConfig struct {
	Mode           ApprovalMode `mapstructure:"mode"`
	TimeoutSeconds int          `mapstructure:"timeout_seconds"`
	RequireNote    bool         `mapstructure:"require_note"`
}

// ClassifyConfig configures the data-classification heuristic.
type ClassifyConfig struct {
	Enabled       bool                `mapstructure:"enabled"`
	ExtraPatterns map[string][]string `mapstructure:"extra_patterns"`
}

// InjectionScoreConfig configures the prompt-injection heuristic.
type InjectionScoreConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// LoggingConfig holds logging-related settings.
type LoggingConfig struct {
	Level           LoggingLevel `mapstructure:"level"`
	StdoutMaxChars  int          `mapstructure:"stdout_max_chars"`
	StderrMaxChars  int          `mapstructure:"stderr_max_chars"`
	ContextMaxChars int          `mapstructure:"context_max_chars"`
	ContentHash     bool         `mapstructure:"content_hash"`
}

// StorageConfig holds storage-related settings.
type StorageConfig struct {
	Path          string `mapstructure:"path"`
	RetentionDays int    `mapstructure:"retention_days"`
}

// PrivacyConfig holds privacy-related settings.
type PrivacyConfig struct {
	SensitivePaths []string `mapstructure:"sensitive_paths"`
	RedactPatterns []string `mapstructure:"redact_patterns"`
}

// FiltersConfig holds content filter settings.
type FiltersConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// AgentConfig holds settings for a specific agent.
type AgentConfig struct {
	Enabled      bool         `mapstructure:"enabled"`
	LoggingLevel LoggingLevel `mapstructure:"logging_level,omitempty"`
}

// AgentsConfig holds per-agent settings.
type AgentsConfig struct {
	ClaudeCode AgentConfig `mapstructure:"claude-code"`
	Cursor     AgentConfig `mapstructure:"cursor"`
	Gemini     AgentConfig `mapstructure:"gemini"`
	OpenCode   AgentConfig `mapstructure:"opencode"`
	OpenClaw   AgentConfig `mapstructure:"openclaw"`
	Windsurf   AgentConfig `mapstructure:"windsurf"`
	PiAgent    AgentConfig `mapstructure:"pi-agent"`
	Codex      AgentConfig `mapstructure:"codex"`
}

// DisplayConfig holds display-related settings.
type DisplayConfig struct {
	Colors   ColorMode    `mapstructure:"colors"`
	Timezone TimezoneMode `mapstructure:"timezone"`
}

// StreamsConfig holds stream target settings.
type StreamsConfig struct {
	Targets []StreamTargetConfig `mapstructure:"targets"`
}

// StreamTargetConfig holds settings for a single stream target.
type StreamTargetConfig struct {
	Name    string         `mapstructure:"name"`
	Type    string         `mapstructure:"type"`
	Enabled bool           `mapstructure:"enabled"`
	Config  map[string]any `mapstructure:"config"`
}

// Paths holds resolved filesystem paths.
type Paths struct {
	ConfigFile   string
	ConfigDir    string
	DataDir      string
	DatabaseFile string
	CacheDir     string
	BackupsDir   string
}

// Load loads configuration from the given path or default locations.
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Set config type
	v.SetConfigType("yaml")

	// Determine config file path
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		paths := ResolvePaths()

		v.SetConfigName("config")
		v.AddConfigPath(paths.ConfigDir)
	}

	// Bind environment variables
	v.SetEnvPrefix("GRYPH")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	// Unmarshal into struct
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error parsing config: %w", err)
	}

	cfg.Policy.Receipts.SignMode = strings.ToLower(strings.TrimSpace(cfg.Policy.Receipts.SignMode))

	if v.InConfig("policy.receipts.sign") && !v.InConfig("policy.receipts.sign_mode") {
		aliased := signModeFromLegacyBool(cfg.Policy.Receipts.Sign)
		signDeprecationOnce.Do(func() {
			log.Warnf("config: policy.receipts.sign is deprecated, use policy.receipts.sign_mode: %v", aliased)
		})
		cfg.Policy.Receipts.SignMode = aliased
	}

	// Validate config
	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func signModeFromLegacyBool(b bool) string {
	if b {
		return SignModeAlways
	}
	return SignModeNever
}

// Default returns a Config with all default values.
func Default() *Config {
	v := viper.New()
	setDefaults(v)

	var cfg Config
	_ = v.Unmarshal(&cfg)

	return &cfg
}

// ResolvePaths returns the resolved filesystem paths for the current platform.
func ResolvePaths() *Paths {
	configDir := getConfigDir()
	dataDir := getDataDir()
	cacheDir := getCacheDir()

	return &Paths{
		ConfigFile:   filepath.Join(configDir, "config.yaml"),
		ConfigDir:    configDir,
		DataDir:      dataDir,
		DatabaseFile: filepath.Join(dataDir, "audit.db"),
		CacheDir:     cacheDir,
		BackupsDir:   filepath.Join(dataDir, "backups"),
	}
}

// GetDatabasePath returns the resolved database path from config or default.
func (c *Config) GetDatabasePath() string {
	if c.Storage.Path != "" {
		return c.Storage.Path
	}

	paths := ResolvePaths()
	return paths.DatabaseFile
}

// EffectivePolicy returns the active policy-layer settings.
func (c *Config) EffectivePolicy() PolicyConfig {
	if c == nil {
		return PolicyConfig{}
	}
	return c.Policy
}

// DefaultReceiptKeyPath returns the on-disk default for the private signing
// key.
func DefaultReceiptKeyPath(paths *Paths) string {
	if paths == nil {
		paths = ResolvePaths()
	}
	return filepath.Join(paths.ConfigDir, "keys", "receipt.key")
}

// DefaultReceiptTrustStorePath returns the on-disk default for the trust
// store JSON.
func DefaultReceiptTrustStorePath(paths *Paths) string {
	if paths == nil {
		paths = ResolvePaths()
	}
	return filepath.Join(paths.ConfigDir, "keys", "receipt-pub.json")
}

// ResolveReceiptKeyPath returns the configured signing-key path or the
// platform default.
func (c *Config) ResolveReceiptKeyPath(paths *Paths) string {
	if c != nil && c.Policy.Receipts.KeyPath != "" {
		return c.Policy.Receipts.KeyPath
	}
	return DefaultReceiptKeyPath(paths)
}

// ResolveReceiptTrustStorePath returns the configured trust store path or the
// platform default.
func (c *Config) ResolveReceiptTrustStorePath(paths *Paths) string {
	if c != nil && c.Policy.Receipts.TrustStore != "" {
		return c.Policy.Receipts.TrustStore
	}
	return DefaultReceiptTrustStorePath(paths)
}

// ShouldUseColors returns true if colors should be used based on config and terminal.
func (c *Config) ShouldUseColors() bool {
	switch c.Display.Colors {
	case ColorAlways:
		return true
	case ColorNever:
		return false
	default:
		// Auto: check if stdout is a terminal
		fileInfo, _ := os.Stdout.Stat()
		return (fileInfo.Mode() & os.ModeCharDevice) != 0
	}
}

// GetAgentLoggingLevel returns the logging level for a specific agent.
// Falls back to global level if not set.
func (c *Config) GetAgentLoggingLevel(agentName string) LoggingLevel {
	switch agentName {
	case agentNameClaudeCode:
		if c.Agents.ClaudeCode.LoggingLevel != "" {
			return c.Agents.ClaudeCode.LoggingLevel
		}
	case agentNameCursor:
		if c.Agents.Cursor.LoggingLevel != "" {
			return c.Agents.Cursor.LoggingLevel
		}
	case agentNameGemini:
		if c.Agents.Gemini.LoggingLevel != "" {
			return c.Agents.Gemini.LoggingLevel
		}
	case agentNameOpenCode:
		if c.Agents.OpenCode.LoggingLevel != "" {
			return c.Agents.OpenCode.LoggingLevel
		}
	case agentNameOpenClaw:
		if c.Agents.OpenClaw.LoggingLevel != "" {
			return c.Agents.OpenClaw.LoggingLevel
		}
	case agentNameWindsurf:
		if c.Agents.Windsurf.LoggingLevel != "" {
			return c.Agents.Windsurf.LoggingLevel
		}
	case agentNamePiAgent:
		if c.Agents.PiAgent.LoggingLevel != "" {
			return c.Agents.PiAgent.LoggingLevel
		}
	case agentNameCodex:
		if c.Agents.Codex.LoggingLevel != "" {
			return c.Agents.Codex.LoggingLevel
		}
	}

	return c.Logging.Level
}

// IsAgentEnabled returns true if the given agent is enabled.
func (c *Config) IsAgentEnabled(agentName string) bool {
	switch agentName {
	case agentNameClaudeCode:
		return c.Agents.ClaudeCode.Enabled
	case agentNameCursor:
		return c.Agents.Cursor.Enabled
	case agentNameGemini:
		return c.Agents.Gemini.Enabled
	case agentNameOpenCode:
		return c.Agents.OpenCode.Enabled
	case agentNameOpenClaw:
		return c.Agents.OpenClaw.Enabled
	case agentNameWindsurf:
		return c.Agents.Windsurf.Enabled
	case agentNamePiAgent:
		return c.Agents.PiAgent.Enabled
	case agentNameCodex:
		return c.Agents.Codex.Enabled
	default:
		return true
	}
}
