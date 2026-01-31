package config

import (
	"github.com/spf13/viper"
)

// setDefaults sets all default configuration values.
func setDefaults(v *viper.Viper) {
	// Logging defaults
	v.SetDefault("logging.level", "minimal")
	v.SetDefault("logging.stdout_max_chars", 1000)
	v.SetDefault("logging.stderr_max_chars", 500)
	v.SetDefault("logging.context_max_chars", 5000)

	// Storage defaults
	v.SetDefault("storage.path", "") // Empty means use platform default
	v.SetDefault("storage.retention_days", 90)

	// Privacy defaults
	v.SetDefault("privacy.hash_file_contents", true)
	v.SetDefault("privacy.sensitive_paths", defaultSensitivePaths())
	v.SetDefault("privacy.redact_patterns", defaultRedactPatterns())

	// Filters defaults
	v.SetDefault("filters.enabled", false)

	// Agent defaults
	v.SetDefault("agents.claude-code.enabled", true)
	v.SetDefault("agents.cursor.enabled", true)

	// Display defaults
	v.SetDefault("display.colors", "auto")
	v.SetDefault("display.timezone", "local")

	// Streams defaults
	v.SetDefault("streams.targets", []StreamTargetConfig{
		{
			Name:    streamTargetTypeStdout,
			Type:    streamTargetTypeStdout,
			Enabled: true,
		},
	})
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
