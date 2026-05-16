package config

import (
	"github.com/spf13/viper"
)

// setDefaults sets all default configuration values.
func setDefaults(v *viper.Viper) {
	// Logging defaults
	v.SetDefault("logging.level", "standard")
	v.SetDefault("logging.stdout_max_chars", 1000)
	v.SetDefault("logging.stderr_max_chars", 500)
	v.SetDefault("logging.context_max_chars", 5000)
	v.SetDefault("logging.content_hash", true)

	// Storage defaults
	v.SetDefault("storage.path", "") // Empty means use platform default
	v.SetDefault("storage.retention_days", 90)

	// Privacy defaults
	v.SetDefault("privacy.sensitive_paths", defaultSensitivePaths())
	v.SetDefault("privacy.redact_patterns", defaultRedactPatterns())

	// Filters defaults
	v.SetDefault("filters.enabled", false)

	// Agent defaults
	v.SetDefault("agents.claude-code.enabled", true)
	v.SetDefault("agents.cursor.enabled", true)
	v.SetDefault("agents.gemini.enabled", true)
	v.SetDefault("agents.opencode.enabled", true)
	v.SetDefault("agents.openclaw.enabled", true)
	v.SetDefault("agents.windsurf.enabled", true)
	v.SetDefault("agents.pi-agent.enabled", true)
	v.SetDefault("agents.codex.enabled", true)

	// Display defaults
	v.SetDefault("display.colors", "auto")
	v.SetDefault("display.timezone", "local")

	setPolicyDefaults(v, "policy")

	// Streams defaults
	v.SetDefault("streams.targets", []StreamTargetConfig{
		{
			Name:    streamTargetTypeNop,
			Type:    streamTargetTypeNop,
			Enabled: true,
		},
	})
}

func setPolicyDefaults(v *viper.Viper, prefix string) {
	v.SetDefault(prefix+".enabled", false)
	v.SetDefault(prefix+".fail_mode", "closed")
	v.SetDefault(prefix+".policy_paths", []string{})
	v.SetDefault(prefix+".conventional_paths", true)
	v.SetDefault(prefix+".context_retention_days", 90)
	v.SetDefault(prefix+".receipt_retention_days", 365)
	v.SetDefault(prefix+".log_all_evaluations", true)
	v.SetDefault(prefix+".approval.mode", string(ApprovalModeNop))
	v.SetDefault(prefix+".approval.timeout_seconds", 60)
	v.SetDefault(prefix+".approval.require_note", false)
	v.SetDefault(prefix+".classify.enabled", true)
	v.SetDefault(prefix+".classify.fail_open", false)
	v.SetDefault(prefix+".classify.extra_patterns", map[string][]string{})
	v.SetDefault(prefix+".injection_score.enabled", true)
	v.SetDefault(prefix+".receipts.sign_mode", SignModeAuto)
	v.SetDefault(prefix+".receipts.key_path", "")
	v.SetDefault(prefix+".receipts.trust_store", "")
	v.SetDefault(prefix+".defer.enabled", true)
	v.SetDefault(prefix+".defer.fresh_session_seconds", 60)
	v.SetDefault(prefix+".defer.conflict_triggers_defer", true)
	v.SetDefault(prefix+".defer.timeout_seconds", 600)
	v.SetDefault(prefix+".defer.auto_resolve_on_timeout", DeferAutoResolveDeny)
}

// defaultSensitivePaths returns the default list of sensitive path patterns.
func defaultSensitivePaths() []string {
	return []string{
		"**/.env",
		"**/.env.*",
		"**/.env.local",
		"**/secrets/**",
		"**/*.pem",
		"**/*.key",
		"**/*.p12",
		"**/*password*",
		"**/*secret*",
		"**/*credential*",
		"**/.git/config",
		"**/.ssh/**",
		"**/.aws/**",
		"**/.npmrc",
		"**/.pypirc",
	}
}

// defaultRedactPatterns returns the default list of redaction regex patterns.
func defaultRedactPatterns() []string {
	return []string{
		`(?i)password[=:]\S+`,
		`(?i)api[_-]?key[=:]\S+`,
		`(?i)token[=:]\S+`,
		`(?i)secret[=:]\S+`,
		`(?i)bearer\s+\S+`,
		`(?i)aws_access_key_id[=:]\S+`,
		`(?i)aws_secret_access_key[=:]\S+`,
	}
}
