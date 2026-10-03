// Package config provides configuration management using Viper.
package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/core/privacy"
	"github.com/spf13/viper"
)

const (
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
	Export  ExportConfig  `mapstructure:"export"`
	Managed ManagedConfig `mapstructure:"managed"`
	// Supervisor is the decision service that runs outside the user. Only
	// the managed file sets it: a user cannot point the hook at a service
	// of their own.
	Supervisor SupervisorConfig `mapstructure:"supervisor"`
	// Collection is what leaves the host for the team. Only the managed
	// file sets it: a user cannot raise or lower what the team collects.
	Collection CollectionConfig `mapstructure:"collection"`
	// ExportKey is the export key file in force when it is not the one
	// next to the database. No file sets it: the decision service sets it
	// to the machine key.
	ExportKey string `mapstructure:"-"`
}

// SupervisorConfig configures the decision service and the hook's use of it.
type SupervisorConfig struct {
	// Enabled turns the hook into a client of the service. A socket alone
	// never does.
	Enabled bool `mapstructure:"enabled"`
	// Socket is the path of the service socket. Empty takes the default of
	// the platform.
	Socket string `mapstructure:"socket"`
	// Profile is enforce or pilot. It decides what a hook does when the
	// service is out of reach.
	Profile string `mapstructure:"profile"`
	// PilotUntil ends the pilot profile: after this date the host runs
	// enforce. A pilot with no end is a standing way to turn signing off,
	// so a managed file that sets pilot sets this too.
	PilotUntil string `mapstructure:"pilot_until"`
	// StateDir holds the partitions of the accounts. Empty takes the
	// default of the platform.
	StateDir string `mapstructure:"state_dir"`
	// SpoolDir is where a hook client leaves what it could not send. Empty
	// takes the default of the platform.
	SpoolDir string `mapstructure:"spool_dir"`
	// Unavailable says what a hook does when the service is out of reach,
	// per fail-mode column. Empty takes the default of the profile.
	Unavailable UnavailableConfig `mapstructure:"unavailable"`
	// ServerIdentity is the account that runs the service. The hook client
	// accepts the socket only when its peer is root or this account. Empty
	// takes the service account of the platform.
	ServerIdentity string `mapstructure:"server_identity"`
	// Fanotify is the kernel watcher that stops a write to the managed
	// files by a process of a non-privileged account. Linux only, off by
	// default.
	Fanotify FanotifyConfig `mapstructure:"fanotify"`
}

// FanotifyConfig configures the fanotify watcher, a separate process in
// its own unit with the capabilities the kernel API needs.
type FanotifyConfig struct {
	Enabled bool `mapstructure:"enabled"`
	// StateFile is where the watcher reports what it protects. Empty puts
	// it next to the socket.
	StateFile string `mapstructure:"state_file"`
}

// FanotifyStatePath returns the state file of the fanotify watcher, which
// gryph doctor reads: the configured path, or fanotify.json next to the
// socket, in the runtime directory every account can read.
func (s SupervisorConfig) FanotifyStatePath() string {
	if s.Fanotify.StateFile != "" {
		return s.Fanotify.StateFile
	}
	return filepath.Join(filepath.Dir(s.SocketPath()), "fanotify.json")
}

// CollectionConfig sets the collection level of a managed host: how much
// of each event leaves the host for the team, through the export profile
// the level names.
type CollectionConfig struct {
	// Level is evidence, policy or full. Empty takes the default of a
	// managed host, policy.
	Level string `mapstructure:"level"`
}

// The collection levels, from the one that sends the least to the one
// that sends the most.
const (
	// CollectionNone is the level of a host with no managed
	// configuration: nothing is collected.
	CollectionNone = "none"
	// CollectionEvidence sends the receipts and the facts of each action
	// with every content value digested. It is always on in a managed
	// host.
	CollectionEvidence = "evidence"
	// CollectionPolicy sends the fields that rules match on, with prompts
	// and content digested and every secret dropped. It is the default.
	CollectionPolicy = "policy"
	// CollectionFull sends every value. A team opts in.
	CollectionFull = "full"
)

// CollectionLevels lists the levels a managed file can set.
var CollectionLevels = []string{CollectionEvidence, CollectionPolicy, CollectionFull}

// EffectiveLevel returns the collection level in force: none without a
// managed configuration, else the configured level or policy.
func (c CollectionConfig) EffectiveLevel() string {
	if !ManagedConfigActive() {
		return CollectionNone
	}
	if c.Level == "" {
		return CollectionPolicy
	}
	return c.Level
}

// Profile returns the export profile that the level in force names:
// metadata for evidence, policy for policy, full for full, and the
// default profile when nothing is collected.
func (c CollectionConfig) Profile() string {
	switch c.EffectiveLevel() {
	case CollectionEvidence:
		return privacy.ProfileMetadata
	case CollectionPolicy:
		return privacy.ProfilePolicy
	case CollectionFull:
		return privacy.ProfileFull
	}
	return privacy.ProfileDefault
}

// SupervisorAccount is the service account of the decision service.
const SupervisorAccount = "_gryph"

// ServerAccount returns the account the hook client expects behind the
// socket.
func (s SupervisorConfig) ServerAccount() string {
	if s.ServerIdentity != "" {
		return s.ServerIdentity
	}
	return SupervisorAccount
}

// UnavailableConfig holds one verdict per fail-mode column: block or
// allow. The enforce profile blocks a blocking hook and allows the rest.
type UnavailableConfig struct {
	Blocking string `mapstructure:"blocking"`
	Prompt   string `mapstructure:"prompt"`
	Other    string `mapstructure:"other"`
}

// The verdicts a hook gives when the service is out of reach.
const (
	UnavailableBlock = "block"
	UnavailableAllow = "allow"
)

// The supervisor profiles.
const (
	SupervisorProfileEnforce = "enforce"
	SupervisorProfilePilot   = "pilot"
)

// EffectiveProfile returns the profile: enforce when unset, and enforce
// when the pilot has passed its end date.
func (s SupervisorConfig) EffectiveProfile() string {
	if s.Profile == SupervisorProfilePilot {
		if until, ok := s.pilotEnd(); ok && !time.Now().Before(until) {
			return SupervisorProfileEnforce
		}
		return SupervisorProfilePilot
	}
	return SupervisorProfileEnforce
}

// PilotRemaining returns the time left in the pilot and true while the
// pilot profile is in force with an end date.
func (s SupervisorConfig) PilotRemaining() (time.Duration, bool) {
	if s.Profile != SupervisorProfilePilot {
		return 0, false
	}
	until, ok := s.pilotEnd()
	if !ok {
		return 0, false
	}
	left := time.Until(until)
	if left <= 0 {
		return 0, false
	}
	return left, true
}

// pilotEnd parses pilot_until: a date (2006-01-02, the end of that day in
// local time) or an RFC 3339 time.
func (s SupervisorConfig) pilotEnd() (time.Time, bool) {
	t, err := ParsePilotUntil(s.PilotUntil)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// ParsePilotUntil parses the value of supervisor.pilot_until. An empty
// value is an error: a pilot needs an end.
func ParsePilotUntil(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, errors.New("no date")
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	day, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is not a date (2006-01-02) or an RFC 3339 time", value)
	}
	return day.AddDate(0, 0, 1), nil
}

// SocketPath returns the socket path, or the default of the platform.
func (s SupervisorConfig) SocketPath() string {
	if s.Socket != "" {
		return s.Socket
	}
	return supervisorSocketDefault()
}

// SpoolPath returns the spool directory, or the default of the platform.
func (s SupervisorConfig) SpoolPath() string {
	if s.SpoolDir != "" {
		return s.SpoolDir
	}
	return supervisorSpoolDefault()
}

// EffectiveUnavailable returns the verdict of the column when the service
// is out of reach. The enforce default blocks a blocking hook, because a
// same-user process can take the service down for its own account, and
// allows a prompt or a lifecycle hook, because a block there stops the
// user and not the agent.
func (s SupervisorConfig) EffectiveUnavailable(column string) string {
	var set string
	switch column {
	case "blocking":
		set = s.Unavailable.Blocking
	case "prompt":
		set = s.Unavailable.Prompt
	default:
		set = s.Unavailable.Other
	}
	if set != "" {
		return set
	}
	if column == "blocking" {
		return UnavailableBlock
	}
	return UnavailableAllow
}

// StatePath returns the state directory, or the default of the platform.
func (s SupervisorConfig) StatePath() string {
	if s.StateDir != "" {
		return s.StateDir
	}
	return supervisorStateDefault()
}

// KeyDir returns the directory of the machine keys: the keys that the
// decision service signs and digests with, owned by the service account.
func (s SupervisorConfig) KeyDir() string {
	return filepath.Join(s.StatePath(), "keys")
}

// ReceiptKeyPath returns the private receipt signing key of the machine.
func (s SupervisorConfig) ReceiptKeyPath() string {
	return filepath.Join(s.KeyDir(), "receipt.key")
}

// PublicKeyPath returns the file that carries the public half of the
// current receipt key, readable by every account, so a root command can
// put it in the managed trust store without reading the private key.
func (s SupervisorConfig) PublicKeyPath() string {
	return filepath.Join(s.KeyDir(), "receipt-pub.json")
}

// ExportKeyPath returns the export key of the machine.
func (s SupervisorConfig) ExportKeyPath() string {
	return filepath.Join(s.KeyDir(), "export.key")
}

// PIDFile returns the file that holds the pid of the running service, so
// a root command can ask it to reload its keys.
func (s SupervisorConfig) PIDFile() string {
	return filepath.Join(s.StatePath(), "supervisor.pid")
}

// ExportConfig holds the user export profiles, by name.
type ExportConfig struct {
	Profiles map[string]ExportProfileConfig `mapstructure:"profiles"`
}

// ExportProfileConfig is one user export profile. See privacy.ExportProfile.
type ExportProfileConfig struct {
	Default     string               `mapstructure:"default"`
	Rules       []privacy.ExportRule `mapstructure:"rules"`
	StripURLs   bool                 `mapstructure:"strip_urls"`
	RedactAgain bool                 `mapstructure:"redact_again"`
}

// ExportProfile returns the export profile with the name. An empty name
// gives the built-in default profile. Names ignore case, because the config
// loader stores every map key in lower case.
//
// An invalid user profile, or a user profile with a built-in name, returns
// an error and never a weaker profile. Load only warns about such a
// profile, so one bad export rule does not reset the hook config.
func (c *Config) ExportProfile(name string) (privacy.ExportProfile, error) {
	name = strings.ToLower(name)
	if name == "" {
		name = privacy.ProfileDefault
	}
	builtin, isBuiltin := privacy.BuiltinProfiles()[name]
	pc, isUser := c.Export.Profiles[name]
	switch {
	case isBuiltin && isUser:
		return privacy.ExportProfile{}, fmt.Errorf("export.profiles.%s: the name is a built-in profile", name)
	case isBuiltin:
		return builtin, nil
	case !isUser:
		return privacy.ExportProfile{}, fmt.Errorf("unknown export profile %q", name)
	}
	p := privacy.ExportProfile{Name: name, Default: privacy.Treatment(pc.Default), Rules: pc.Rules, StripURLs: pc.StripURLs, RedactAgain: pc.RedactAgain}
	if err := p.Validate(); err != nil {
		return privacy.ExportProfile{}, err
	}
	return p, nil
}

// PolicyConfig holds Gryph policy-layer settings.
type PolicyConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	FailMode string `mapstructure:"fail_mode"`
	// AllowUserPolicy, in a system managed configuration, keeps the user's
	// own policy sources in the merge. It is true by default, because a new
	// file can only add rules. An administrator sets it to false to make
	// the managed policy the whole policy. It has no effect outside a
	// managed configuration.
	AllowUserPolicy bool `mapstructure:"allow_user_policy"`

	ContextRetentionDays int  `mapstructure:"context_retention_days"`
	ReceiptRetentionDays int  `mapstructure:"receipt_retention_days"`
	LogAllEvaluations    bool `mapstructure:"log_all_evaluations"`
	// ShellBudget bounds the shell analysis of one command in the hook. A
	// command that runs past it matches every path, host and hook rule.
	ShellBudget time.Duration `mapstructure:"shell_budget"`

	Approval       ApprovalConfig       `mapstructure:"approval"`
	Classify       ClassifyConfig       `mapstructure:"classify"`
	InjectionScore InjectionScoreConfig `mapstructure:"injection_score"`
	Receipts       ReceiptsConfig       `mapstructure:"receipts"`
	Defer          DeferConfig          `mapstructure:"defer"`
	Identity       IdentityConfig       `mapstructure:"identity"`
	SelfProtection SelfProtectionConfig `mapstructure:"self_protection"`
	Context        ContextConfig        `mapstructure:"context"`
}

// ContextConfig controls what the session context gives to policy and to a
// window. CELEntries is the number of the latest entries in context.entries.
type ContextConfig struct {
	CELEntries int `mapstructure:"cel_entries"`
	// WindowMaxEntries and WindowMaxBytes are the default size of a window
	// of the session context.
	WindowMaxEntries int `mapstructure:"window_max_entries"`
	WindowMaxBytes   int `mapstructure:"window_max_bytes"`
}

// MaxCELEntries bounds context.entries, so one rule cannot load a whole
// session into every evaluation.
const MaxCELEntries = 1000

// MaxWindowEntries bounds a window, so one call cannot load a whole session.
const MaxWindowEntries = 1000

// DefaultWindowMaxBytes is the default byte bound of a window.
const DefaultWindowMaxBytes = 65536

// SelfProtectionConfig toggles the built-in rules that block agent writes to
// Gryph's policy files, database, keys, and the agents' hook configs. Honored
// only from the operator-owned config file, never a repo-local policy.
type SelfProtectionConfig struct {
	Enabled bool `mapstructure:"enabled"`
	// Repair lets a reconcile pass rewrite a hook configuration that
	// differs from a current install. It is off by default in the user
	// scope, and on by default under a system managed configuration.
	Repair bool `mapstructure:"repair"`
	// Census turns the process census on. It is on by default. Turn it off
	// on a host where an agent runs for another reason than the user's own
	// work, for example a build host.
	Census bool `mapstructure:"census"`
	// CensusWindow is how long a live agent process may run without a hook
	// call before the census reports it as silent.
	CensusWindow time.Duration `mapstructure:"census_window"`
}

// DefaultCensusWindow is the census window when the config sets none.
const DefaultCensusWindow = 10 * time.Minute

// EffectiveCensusWindow returns the census window, or the default when the
// config holds none or a value that is not positive.
func (c SelfProtectionConfig) EffectiveCensusWindow() time.Duration {
	if c.CensusWindow <= 0 {
		return DefaultCensusWindow
	}
	return c.CensusWindow
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
	Sign     bool   `mapstructure:"sign"`
	SignMode string `mapstructure:"sign_mode"`
	// KeyScope is the scope the receipts of this process carry. No file
	// sets it: the decision service sets it on the machine key.
	KeyScope   string `mapstructure:"-"`
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
// Mode, TimeoutSeconds and RequireNote drive the prompt of a hook that
// decides in process. The other keys drive the decision service, which
// keeps a request store and answers through channels.
type ApprovalConfig struct {
	Mode           ApprovalMode `mapstructure:"mode"`
	TimeoutSeconds int          `mapstructure:"timeout_seconds"`
	RequireNote    bool         `mapstructure:"require_note"`

	// Channels names the approval channels the decision service uses, by
	// the assurance each one gives.
	Channels []string `mapstructure:"channels"`
	// MinAssurance is the floor for an escalate rule that sets none.
	MinAssurance string `mapstructure:"min_assurance"`
	// InlineWait bounds how long a hook waits for an inline answer. The
	// client bounds it again by the hook timeout of the agent.
	InlineWait time.Duration `mapstructure:"inline_wait"`
	// RequestTTL is how long an unanswered request stays open. It then
	// expires as a deny.
	RequestTTL time.Duration `mapstructure:"request_ttl"`
	// GrantTTL is how long a stored approval stays usable.
	GrantTTL time.Duration `mapstructure:"grant_ttl"`
	// MaxGrantScope is the widest scope an approver can give: once,
	// session or window.
	MaxGrantScope string `mapstructure:"max_grant_scope"`
	// LocalAdmin configures the local-admin channel.
	LocalAdmin LocalAdminConfig `mapstructure:"local_admin"`
}

// LocalAdminConfig names who answers on the local-admin channel.
type LocalAdminConfig struct {
	// Group is the group whose members answer. Empty leaves the channel
	// with nobody.
	Group string `mapstructure:"group"`
	// AllowSelfElevated accepts an answer from the person who asked, through
	// another account of theirs, at the self-elevated assurance.
	AllowSelfElevated bool `mapstructure:"allow_self_elevated"`
	// AllowWithoutAuth accepts an answer without the password of the
	// approver on a host whose authority could ask for it. Off, an
	// answer needs the password when the host has polkit.
	AllowWithoutAuth bool `mapstructure:"allow_without_auth"`
}

// The approval channels, named by the assurance each one gives.
const (
	ApprovalChannelSameUserTTY  = "same-user-tty"
	ApprovalChannelSelfElevated = "self-elevated"
	ApprovalChannelLocalAdmin   = "local-admin"
	ApprovalChannelLocalAuth    = "local-auth"
	ApprovalChannelOutOfBand    = "out-of-band"
)

// ApprovalChannels lists every channel the configuration accepts.
var ApprovalChannels = []string{ApprovalChannelSameUserTTY, ApprovalChannelSelfElevated, ApprovalChannelLocalAdmin, ApprovalChannelLocalAuth, ApprovalChannelOutOfBand}

// The approval grant scopes, narrowest first.
const (
	ApprovalScopeOnce    = "once"
	ApprovalScopeSession = "session"
	ApprovalScopeWindow  = "window"
)

// ApprovalScopes lists every scope the configuration accepts.
var ApprovalScopes = []string{ApprovalScopeOnce, ApprovalScopeSession, ApprovalScopeWindow}

// The defaults of the decision service's approval keys.
const (
	DefaultApprovalInlineWait = 15 * time.Second
	DefaultApprovalRequestTTL = 30 * time.Minute
	DefaultApprovalGrantTTL   = 15 * time.Minute
	// MinApprovalInlineWait is the shortest wait a client makes. A shorter
	// budget skips the wait and blocks with the request pending.
	MinApprovalInlineWait = 2 * time.Second
)

// HasChannel reports whether the configuration names channel.
func (a ApprovalConfig) HasChannel(channel string) bool {
	return slices.Contains(a.Channels, channel)
}

// ClassifyConfig configures the data-classification heuristic.
//
// FailOpen toggles the AARM safe-by-default classification safety net. When
// false (the default and AARM-conformant), the mediation adapter appends
// privacy.ClassUnknownSensitive to any action the classifier left
// unlabeled so policies that gate on classification fail safe. When true,
// the adapter skips the safety-net label so an unlabeled action carries an
// empty list. Operators who explicitly want classification off and do not
// want the fail-safe label flip this to true.
type ClassifyConfig struct {
	Enabled  bool `mapstructure:"enabled"`
	FailOpen bool `mapstructure:"fail_open"`
	// ExtraPatterns adds globs to built-in classes. Each key must be a
	// privacy.Class. A class is never a config value, so the classifier skips
	// an unknown key with a warning. An error would make the CLI fall back to
	// the default config and lose every other setting.
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

// AgentsConfig holds per-agent settings keyed by agent name. The keys match
// the adapter names from agent registration, e.g. "claude-code".
type AgentsConfig map[string]AgentConfig

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
	// ExportProfile names the export profile of the target. Empty gives
	// the built-in default profile.
	ExportProfile string `mapstructure:"export_profile"`
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
// Precedence: the system managed file is authoritative when present. It
// wins over an explicit configPath and over the per-user config file, so a
// user cannot bypass it with --config. Gryph also reads no GRYPH_* variable
// while it is active. Without a managed file, an explicit configPath wins
// over the per-user file, and GRYPH_* variables win over both.
func Load(configPath string) (*Config, error) {
	MigrateLegacyLayout()

	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Set config type
	v.SetConfigType("yaml")

	// Determine config file path
	managed := ""
	if managed = ManagedConfigFile(); managed != "" {
		v.SetConfigFile(managed)
		// An administrator who manages the host wants the hooks to stay in
		// place. A user who installed Gryph for themself opts in.
		v.SetDefault("policy.self_protection.repair", true)
	} else if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		paths := ResolvePaths()

		v.SetConfigName("config")
		v.AddConfigPath(paths.ConfigDir)
	}

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

	// The managed file is the only source of settings while it is active. A
	// GRYPH_* variable would let a user override the administrator.
	if managed == "" {
		if err := applyEnvOverrides(v); err != nil {
			return nil, err
		}
	}

	return unmarshalConfig(v)
}

// unmarshalConfig turns the read sources of v into a validated Config.
func unmarshalConfig(v *viper.Viper) (*Config, error) {
	normalizeShellBudget(v)
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

	clampContext(v, &cfg.Policy.Context)

	// Validate config
	if err := validate(&cfg); err != nil {
		return nil, err
	}
	for _, err := range exportProfileErrors(&cfg) {
		log.Warnf("config: %v. Gryph does not export with this profile.", err)
	}

	return &cfg, nil
}

const (
	shellBudgetKey = "policy.shell_budget"
	// DefaultShellBudget is the default of policy.shell_budget.
	DefaultShellBudget = 500 * time.Millisecond
	// MinShellBudget is the smallest policy.shell_budget. A smaller budget
	// makes most commands run past it, and the hook then blocks them.
	MinShellBudget = 10 * time.Millisecond
)

// shellBudgetValue parses a raw policy.shell_budget. A bare number has no
// unit, and the decoder would read 500 as 500ns, so it is an error.
func shellBudgetValue(raw any) (time.Duration, error) {
	s, ok := raw.(string)
	if !ok {
		return 0, fmt.Errorf("%s must be a duration with a unit, such as 500ms", shellBudgetKey)
	}
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("%s: %w", shellBudgetKey, err)
	}
	if d < MinShellBudget {
		return 0, fmt.Errorf("%s must be %s or more", shellBudgetKey, MinShellBudget)
	}
	return d, nil
}

// normalizeShellBudget replaces an invalid policy.shell_budget with the
// default before the decode. A decode or validation error makes loadApp
// fall back to the defaults, and the defaults turn the policy off. gryph
// config set still rejects an invalid value, see validateSettings.
func normalizeShellBudget(v *viper.Viper) {
	if _, err := shellBudgetValue(v.Get(shellBudgetKey)); err != nil {
		log.Warnf("config: %v. Gryph uses %s instead of %v", err, DefaultShellBudget, v.Get(shellBudgetKey))
		v.Set(shellBudgetKey, DefaultShellBudget.String())
	}
}

// contextLimit is the valid range of one policy.context key. An
// out-of-range value moves to fallback, or to the nearest bound when
// fallback is 0.
type contextLimit struct {
	key      string
	field    func(*ContextConfig) *int
	lo, hi   int
	fallback int
}

var contextLimits = []contextLimit{
	{
		key:   "policy.context.cel_entries",
		field: func(c *ContextConfig) *int { return &c.CELEntries },
		lo:    1, hi: MaxCELEntries,
	},
	{
		key:   "policy.context.window_max_entries",
		field: func(c *ContextConfig) *int { return &c.WindowMaxEntries },
		lo:    1, hi: MaxWindowEntries,
	},
	{
		// 0 means no byte limit. So a negative value falls back to the
		// default and does not move to the lower bound.
		key:   "policy.context.window_max_bytes",
		field: func(c *ContextConfig) *int { return &c.WindowMaxBytes },
		lo:    0, hi: math.MaxInt, fallback: DefaultWindowMaxBytes,
	},
}

// inRange also checks the raw value. The decoder truncates a fraction, so
// -0.5 becomes 0, and 0 means no byte limit.
func (l contextLimit) inRange(v *viper.Viper, cfg *ContextConfig) bool {
	n := *l.field(cfg)
	if n < l.lo || n > l.hi {
		return false
	}
	f, ok := v.Get(l.key).(float64)
	return !ok || (f >= float64(l.lo) && f <= float64(l.hi))
}

func (l contextLimit) rangeError() error {
	if l.hi == math.MaxInt {
		return fmt.Errorf("%s must be %d or more", l.key, l.lo)
	}
	return fmt.Errorf("%s must be between %d and %d", l.key, l.lo, l.hi)
}

// clampContext moves an out-of-range policy.context value into its range. A
// load error makes loadApp fall back to the defaults, and the defaults turn
// the policy off. gryph config set still rejects the key that it sets, see
// checkContextKey.
func clampContext(v *viper.Viper, cfg *ContextConfig) {
	for _, l := range contextLimits {
		if l.inRange(v, cfg) {
			continue
		}
		p := l.field(cfg)
		n := l.fallback
		if n == 0 {
			n = min(max(*p, l.lo), l.hi)
		}
		log.Warnf("config: %v. Gryph uses %d instead of %v", l.rangeError(), n, v.Get(l.key))
		*p = n
	}
}

// checkContextKey rejects an out-of-range value of key.
func checkContextKey(v *viper.Viper, key string, cfg *ContextConfig) error {
	for _, l := range contextLimits {
		if l.key == key && !l.inRange(v, cfg) {
			return l.rangeError()
		}
	}
	return nil
}

func signModeFromLegacyBool(b bool) string {
	if b {
		return SignModeAlways
	}
	return SignModeNever
}

// applyEnvOverrides lets GRYPH_* variables override the defaults and the
// config file. viper's Unmarshal does not consult environment variables,
// only Get does (spf13/viper#761). AllKeys only reports keys with a default,
// a file entry, or an explicit binding, so bind every Config key first. Then
// materialize each key through Get so a GRYPH_* variable reaches the struct,
// including optional keys absent from defaults and file.
func applyEnvOverrides(v *viper.Viper) error {
	v.SetEnvPrefix("GRYPH")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := bindStructEnvKeys(v, reflect.TypeOf(Config{}), ""); err != nil {
		return fmt.Errorf("bind config env keys: %w", err)
	}
	for _, key := range v.AllKeys() {
		v.Set(key, v.Get(key))
	}
	return nil
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
		// A map field has no static keys to walk. Bind the value-struct
		// leaves under each key viper already knows from defaults or the
		// config file, so env overrides keep working for those entries.
		if fieldType.Kind() == reflect.Map {
			elem := fieldType.Elem()
			if elem.Kind() == reflect.Pointer {
				elem = elem.Elem()
			}
			if elem.Kind() != reflect.Struct {
				continue
			}
			for _, name := range knownMapKeys(v, key) {
				if err := bindStructEnvKeys(v, elem, key+"."+name); err != nil {
					return err
				}
			}
			continue
		}
		if err := v.BindEnv(key); err != nil {
			return err
		}
	}
	return nil
}

// knownMapKeys returns the child key names viper knows under prefix, from
// defaults and the config file.
func knownMapKeys(v *viper.Viper, prefix string) []string {
	seen := make(map[string]bool)
	var keys []string
	p := prefix + "."
	for _, k := range v.AllKeys() {
		rest, ok := strings.CutPrefix(k, p)
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(rest, ".")
		if !seen[name] {
			seen[name] = true
			keys = append(keys, name)
		}
	}
	return keys
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

// ReceiptTrustStorePaths returns the trust stores a verifier loads: the
// configured or default store, and the managed store when it exists and is
// another file. The managed store passes the path chain check, so a key in
// it is one the administrator trusts.
func (c *Config) ReceiptTrustStorePaths(paths *Paths) []string {
	out := []string{c.ResolveReceiptTrustStorePath(paths)}
	managed := ManagedTrustStorePath()
	if managed == "" || managed == out[0] {
		return out
	}
	if _, err := os.Stat(managed); err != nil {
		return out
	}
	if err := managedPathTrusted(managed); err != nil {
		return out
	}
	return append(out, managed)
}

// WritableTrustStorePath returns the trust store that a key command of the
// user writes. The managed store is root's, so when the configuration
// points at it the user's own store takes the write.
func (c *Config) WritableTrustStorePath(paths *Paths) string {
	path := c.ResolveReceiptTrustStorePath(paths)
	if managed := ManagedTrustStorePath(); managed != "" && path == managed {
		return DefaultReceiptTrustStorePath(paths)
	}
	return path
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
	if ac, ok := c.Agents[agentName]; ok && ac.LoggingLevel != "" {
		return ac.LoggingLevel
	}
	return c.Logging.Level
}

// IsAgentEnabled returns true if the given agent is enabled.
// An agent without a config entry is enabled.
func (c *Config) IsAgentEnabled(agentName string) bool {
	if ac, ok := c.Agents[agentName]; ok {
		return ac.Enabled
	}
	return true
}
