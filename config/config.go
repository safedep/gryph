// Package config provides configuration management using Viper.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

	ContextRetentionDays int  `mapstructure:"context_retention_days"`
	ReceiptRetentionDays int  `mapstructure:"receipt_retention_days"`
	LogAllEvaluations    bool `mapstructure:"log_all_evaluations"`

	Approval       ApprovalConfig       `mapstructure:"approval"`
	Classify       ClassifyConfig       `mapstructure:"classify"`
	InjectionScore InjectionScoreConfig `mapstructure:"injection_score"`
	Receipts       ReceiptsConfig       `mapstructure:"receipts"`
	Defer          DeferConfig          `mapstructure:"defer"`
	Identity       IdentityConfig       `mapstructure:"identity"`
	SelfProtection SelfProtectionConfig `mapstructure:"self_protection"`
}

// SelfProtectionConfig toggles the built-in rules that block agent writes to
// Gryph's policy files, database, keys, and the agents' hook configs. Honored
// only from the operator-owned config file, never a repo-local policy.
type SelfProtectionConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// IdentityConfig controls the AARM identity-capture layer. Enabled gates the
// capturer entirely. RequireHumanPrincipal turns a missing principal into a
// pre-PDP block ("Action denied: no verifiable human principal").
type IdentityConfig struct {
	Enabled               bool `mapstructure:"enabled"`
	RequireHumanPrincipal bool `mapstructure:"require_human_principal"`
}

// DeferConfig configures the deferral service. Enabled gates both the
// fresh-session and conflicting-policies synthetic defer triggers and the
// timeout sweep. AutoResolveOnTimeout is constrained to "deny" by AARM R4:
// timed-out deferrals must never resolve to allow.
type DeferConfig struct {
	Enabled               bool   `mapstructure:"enabled"`
	FreshSessionSeconds   int    `mapstructure:"fresh_session_seconds"`
	ConflictTriggersDefer bool   `mapstructure:"conflict_triggers_defer"`
	TimeoutSeconds        int    `mapstructure:"timeout_seconds"`
	AutoResolveOnTimeout  string `mapstructure:"auto_resolve_on_timeout"`
}

// DeferAutoResolveDeny is the only valid value for
// DeferConfig.AutoResolveOnTimeout. AARM R4 forbids implicit allow on
// deferral timeout, so this constant is the single supported outcome.
const DeferAutoResolveDeny = "deny"

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
//
// FailOpen toggles the AARM safe-by-default classification safety net. When
// false (the default and AARM-conformant), the mediation adapter appends
// classify.LabelUnknownSensitive to any action the classifier left
// unlabeled so policies that gate on classification fail safe. When true,
// the adapter skips the safety-net label so an unlabeled action carries an
// empty list. Operators who explicitly want classification off and do not
// want the fail-safe label flip this to true.
type ClassifyConfig struct {
	Enabled       bool                `mapstructure:"enabled"`
	FailOpen      bool                `mapstructure:"fail_open"`
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
//
// Precedence: an explicit configPath wins, then the system managed file,
// then the per-user config file.
func Load(configPath string) (*Config, error) {
	MigrateLegacyLayout()

	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Set config type
	v.SetConfigType("yaml")

	// Determine config file path
	managed := ""
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else if managed = ManagedConfigFile(); managed != "" {
		v.SetConfigFile(managed)
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
			// A broken managed file makes the caller fall back to defaults.
			// Name the file so the administrator can find it.
			if managed != "" {
				log.Warnf("failed to read managed config %s: %v", managed, err)
			}
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	// viper's Unmarshal does not consult environment variables, only Get does
	// (spf13/viper#761). AllKeys only reports keys with a default, a file
	// entry, or an explicit binding, so bind every Config key first. Then
	// materialize each key through Get so a GRYPH_* variable reaches the
	// struct, including optional keys absent from defaults and file.
	if err := bindStructEnvKeys(v, reflect.TypeOf(Config{}), ""); err != nil {
		return nil, fmt.Errorf("bind config env keys: %w", err)
	}
	for _, key := range v.AllKeys() {
		v.Set(key, v.Get(key))
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

// bindStructEnvKeys walks the mapstructure tags of t and binds each leaf key
// with viper, so the key shows up in AllKeys even without a default or a
// file entry.
func bindStructEnvKeys(v *viper.Viper, t reflect.Type, prefix string) error {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag, _, _ := strings.Cut(field.Tag.Get("mapstructure"), ",")
		if tag == "" || tag == "-" {
			continue
		}
		key := tag
		if prefix != "" {
			key = prefix + "." + tag
		}
		fieldType := field.Type
		if fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		if fieldType.Kind() == reflect.Struct {
			if err := bindStructEnvKeys(v, fieldType, key); err != nil {
				return err
			}
			continue
		}
		if err := v.BindEnv(key); err != nil {
			return err
		}
	}
	return nil
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
		ConfigFile:   filepath.Join(configDir, configFileName),
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

// DefaultPolicyFilePath returns the on-disk default for the global policy
// file.
func DefaultPolicyFilePath(paths *Paths) string {
	if paths == nil {
		paths = ResolvePaths()
	}
	return filepath.Join(paths.ConfigDir, "policy.yaml")
}

// DefaultPolicyDirPath returns the on-disk default for the policies directory.
// Files in this directory merge after the global file and before the built-in
// rules. The name is a fixed convention, not a settable path.
func DefaultPolicyDirPath(paths *Paths) string {
	if paths == nil {
		paths = ResolvePaths()
	}
	return filepath.Join(paths.ConfigDir, "policies")
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
