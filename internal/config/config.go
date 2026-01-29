package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Logging  LoggingConfig
	Storage  StorageConfig
	Privacy  PrivacyConfig
	Filters  FiltersConfig
	Agents   AgentsConfig
	Display  DisplayConfig
	Sync     SyncConfig
	Paths    Paths
	Defaults DefaultDetails
}

type LoggingConfig struct {
	Level           string
	StdoutMaxChars  int
	StderrMaxChars  int
	ContextMaxChars int
}

type StorageConfig struct {
	Path          string
	RetentionDays int
}

type PrivacyConfig struct {
	HashFileContents bool
	SensitivePaths   []string
	RedactPatterns   []string
}

type FiltersConfig struct {
	Enabled bool
}

type AgentsConfig struct {
	ClaudeCode AgentSettings
	Cursor     AgentSettings
}

type AgentSettings struct {
	Enabled bool
}

type DisplayConfig struct {
	Colors   string
	Timezone string
}

type SyncConfig struct {
	Enabled bool
}

type Paths struct {
	ConfigFile string
	DataDir    string
	Database   string
	CacheDir   string
	BackupsDir string
	ConfigDir  string
}

type DefaultDetails struct {
	DataDir string
}

func Default() *Config {
	return &Config{
		Logging: LoggingConfig{
			Level:           "minimal",
			StdoutMaxChars:  1000,
			StderrMaxChars:  500,
			ContextMaxChars: 5000,
		},
		Storage: StorageConfig{
			Path:          "",
			RetentionDays: 90,
		},
		Privacy: PrivacyConfig{
			HashFileContents: true,
			SensitivePaths: []string{
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
			},
			RedactPatterns: []string{
				"(?i)password[=:]\\S+",
				"(?i)api[_-]?key[=:]\\S+",
				"(?i)token[=:]\\S+",
				"(?i)secret[=:]\\S+",
				"(?i)bearer\\s+\\S+",
				"(?i)aws_access_key_id[=:]\\S+",
				"(?i)aws_secret_access_key[=:]\\S+",
			},
		},
		Filters: FiltersConfig{Enabled: false},
		Agents: AgentsConfig{
			ClaudeCode: AgentSettings{Enabled: true},
			Cursor:     AgentSettings{Enabled: true},
		},
		Display: DisplayConfig{Colors: "auto", Timezone: "local"},
		Sync:    SyncConfig{Enabled: false},
	}
}

func Load(configPath string) (*Config, error) {
	cfg := Default()
	paths, err := ResolvePaths()
	if err != nil {
		return nil, err
	}
	cfg.Paths = paths
	cfg.Paths.ConfigFile = paths.ConfigFile
	cfg.Paths.Database = resolveDatabasePath(cfg, paths)
	cfg.Paths.BackupsDir = filepath.Join(paths.DataDir, "backups")
	cfg.Defaults.DataDir = paths.DataDir

	if configPath == "" {
		configPath = paths.ConfigFile
	}
	cfg.Paths.ConfigFile = configPath

	if err := readConfigFile(configPath, cfg); err != nil {
		return nil, err
	}
	cfg.Paths.Database = resolveDatabasePath(cfg, paths)
	return cfg, nil
}

func Write(configPath string, cfg *Config) error {
	if configPath == "" {
		configPath = cfg.Paths.ConfigFile
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return fmt.Errorf("ensure config dir: %w", err)
	}
	content := renderConfig(cfg)
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func ResolvePaths() (Paths, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, fmt.Errorf("config dir: %w", err)
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return Paths{}, fmt.Errorf("cache dir: %w", err)
	}
	dataDir, err := resolveDataDir()
	if err != nil {
		return Paths{}, err
	}

	return Paths{
		ConfigDir:  configDir,
		ConfigFile: filepath.Join(configDir, "gryph", "config.yaml"),
		DataDir:    dataDir,
		Database:   filepath.Join(dataDir, "audit.db"),
		CacheDir:   filepath.Join(cacheDir, "gryph"),
	}, nil
}

func resolveDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "gryph"), nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config dir: %w", err)
	}
	lower := strings.ToLower(configDir)
	switch {
	case strings.Contains(lower, "appdata"):
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "gryph"), nil
	case strings.Contains(lower, "library"):
		return filepath.Join(configDir, "gryph"), nil
	default:
		return filepath.Join(home, ".local", "share", "gryph"), nil
	}
}

func resolveDatabasePath(cfg *Config, paths Paths) string {
	if cfg.Storage.Path != "" {
		return cfg.Storage.Path
	}
	return paths.Database
}

func readConfigFile(path string, cfg *Config) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read config: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	section := ""
	var listTarget *[]string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasSuffix(line, ":") {
			section = strings.TrimSuffix(line, ":")
			listTarget = nil
			continue
		}
		if strings.HasPrefix(line, "-") && listTarget != nil {
			item := strings.TrimSpace(strings.TrimPrefix(line, "-"))
			*listTarget = append(*listTarget, strings.Trim(item, "\""))
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		value = strings.Trim(value, "\"")

		switch section {
		case "logging":
			if key == "level" {
				cfg.Logging.Level = value
			}
		case "storage":
			if key == "path" {
				cfg.Storage.Path = value
			}
			if key == "retention_days" {
				fmt.Sscanf(value, "%d", &cfg.Storage.RetentionDays)
			}
		case "privacy":
			switch key {
			case "hash_file_contents":
				cfg.Privacy.HashFileContents = value == "true"
			case "sensitive_paths":
				listTarget = &cfg.Privacy.SensitivePaths
			case "redact_patterns":
				listTarget = &cfg.Privacy.RedactPatterns
			}
		case "display":
			if key == "colors" {
				cfg.Display.Colors = value
			}
			if key == "timezone" {
				cfg.Display.Timezone = value
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan config: %w", err)
	}
	return nil
}

func renderConfig(cfg *Config) string {
	builder := &strings.Builder{}
	builder.WriteString("logging:\n")
	builder.WriteString(fmt.Sprintf("  level: %s\n", cfg.Logging.Level))
	builder.WriteString(fmt.Sprintf("  stdout_max_chars: %d\n", cfg.Logging.StdoutMaxChars))
	builder.WriteString(fmt.Sprintf("  stderr_max_chars: %d\n", cfg.Logging.StderrMaxChars))
	builder.WriteString(fmt.Sprintf("  context_max_chars: %d\n\n", cfg.Logging.ContextMaxChars))

	builder.WriteString("storage:\n")
	builder.WriteString(fmt.Sprintf("  path: %s\n", cfg.Storage.Path))
	builder.WriteString(fmt.Sprintf("  retention_days: %d\n\n", cfg.Storage.RetentionDays))

	builder.WriteString("privacy:\n")
	builder.WriteString(fmt.Sprintf("  hash_file_contents: %t\n", cfg.Privacy.HashFileContents))
	builder.WriteString("  sensitive_paths:\n")
	for _, entry := range cfg.Privacy.SensitivePaths {
		builder.WriteString(fmt.Sprintf("    - %s\n", entry))
	}
	builder.WriteString("  redact_patterns:\n")
	for _, entry := range cfg.Privacy.RedactPatterns {
		builder.WriteString(fmt.Sprintf("    - %s\n", entry))
	}
	builder.WriteString("\nfilters:\n")
	builder.WriteString(fmt.Sprintf("  enabled: %t\n", cfg.Filters.Enabled))
	builder.WriteString("\nagents:\n")
	builder.WriteString(fmt.Sprintf("  claude-code:\n    enabled: %t\n", cfg.Agents.ClaudeCode.Enabled))
	builder.WriteString(fmt.Sprintf("  cursor:\n    enabled: %t\n", cfg.Agents.Cursor.Enabled))
	builder.WriteString("\ndisplay:\n")
	builder.WriteString(fmt.Sprintf("  colors: %s\n", cfg.Display.Colors))
	builder.WriteString(fmt.Sprintf("  timezone: %s\n", cfg.Display.Timezone))
	builder.WriteString("\nsync:\n")
	builder.WriteString(fmt.Sprintf("  enabled: %t\n", cfg.Sync.Enabled))
	return builder.String()
}
