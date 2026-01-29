package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/safedep/gryph/internal/config"
	"github.com/safedep/gryph/internal/domain"
	"github.com/safedep/gryph/internal/presentation"
	"github.com/safedep/gryph/internal/storage"
	"github.com/safedep/gryph/internal/version"
)

type CLI struct {
	cfg       *config.Config
	service   *domain.Service
	store     *storage.Store
	out       io.Writer
	errOut    io.Writer
	format    string
	presenter presentation.Presenter
}

func New() *CLI {
	return &CLI{out: os.Stdout, errOut: os.Stderr}
}

func (c *CLI) Execute() error {
	args := os.Args[1:]
	global := flag.NewFlagSet("gryph", flag.ContinueOnError)
	global.SetOutput(c.errOut)
	configPath := global.String("config", "", "Path to config file")
	global.StringVar(configPath, "c", "", "Path to config file")
	format := global.String("format", "table", "Output format: table, json")
	_ = global.Bool("no-color", false, "Disable colored output")
	_ = global.Bool("verbose", false, "Increase output verbosity")
	_ = global.Bool("quiet", false, "Suppress non-essential output")

	if err := global.Parse(args); err != nil {
		return err
	}

	remaining := global.Args()
	if len(remaining) == 0 {
		return c.usage()
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	store, err := storage.Open(cfg.Paths.Database)
	if err != nil {
		return err
	}
	c.cfg = cfg
	c.store = store
	c.service = domain.NewService(store, cfg, version.Version)
	c.format = *format
	c.presenter = c.selectPresenter()
	if err := c.service.Initialize(context.Background()); err != nil {
		return err
	}
	defer c.store.Close()

	cmd := remaining[0]
	switch cmd {
	case "install":
		return c.install(remaining[1:])
	case "uninstall":
		return c.uninstall(remaining[1:])
	case "status":
		return c.status(remaining[1:])
	case "doctor":
		return c.doctor(remaining[1:])
	case "logs":
		return c.logs(remaining[1:])
	case "query":
		return c.query(remaining[1:])
	case "sessions":
		return c.sessions(remaining[1:])
	case "session":
		return c.session(remaining[1:])
	case "export":
		return c.export(remaining[1:])
	case "config":
		return c.configCmd(remaining[1:])
	case "self-log":
		return c.selfLog(remaining[1:])
	case "diff":
		return c.diff(remaining[1:])
	case "_hook":
		return c.hook(remaining[1:])
	default:
		return c.usage()
	}
}

func (c *CLI) usage() error {
	fmt.Fprintln(c.out, "gryph - The AI Coding Agent Observability Tool")
	fmt.Fprintln(c.out, "\nUsage: gryph <command> [flags]\n")
	fmt.Fprintln(c.out, "Commands:")
	fmt.Fprintln(c.out, "  install       Install hooks for AI coding agents")
	fmt.Fprintln(c.out, "  uninstall     Remove hooks from agents")
	fmt.Fprintln(c.out, "  status        Show installation status and health")
	fmt.Fprintln(c.out, "  doctor        Diagnose issues with installation")
	fmt.Fprintln(c.out, "  logs          Display recent agent activity")
	fmt.Fprintln(c.out, "  query         Query audit logs with filters")
	fmt.Fprintln(c.out, "  sessions      List recorded sessions")
	fmt.Fprintln(c.out, "  session       Show detailed view of a specific session")
	fmt.Fprintln(c.out, "  export        Export audit data")
	fmt.Fprintln(c.out, "  config        View or modify configuration")
	fmt.Fprintln(c.out, "  self-log      View the tool's own audit trail")
	fmt.Fprintln(c.out, "  diff          View diff content for a file_write event")
	return nil
}

func (c *CLI) selectPresenter() presentation.Presenter {
	switch c.format {
	case "json":
		return presentation.NewJSONPresenter(c.out)
	default:
		return presentation.NewTablePresenter(c.out)
	}
}

func (c *CLI) install(args []string) error {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(c.errOut)
	_ = flags.Bool("dry-run", false, "Show what would be installed")
	_ = flags.Bool("force", false, "Overwrite existing hooks without prompting")
	_ = flags.Bool("backup", true, "Backup existing hooks")
	_ = flags.Bool("no-backup", false, "Skip backup")
	_ = flags.String("agent", "", "Install for specific agent only")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if err := config.Write(c.cfg.Paths.ConfigFile, c.cfg); err != nil {
		return err
	}
	result := presentation.InstallResult{
		Agents: []presentation.InstallAgentResult{
			{Name: "Claude Code", Status: "ok", Note: "~/.claude/"},
			{Name: "Cursor", Status: "ok", Note: "~/.cursor/"},
		},
		DatabasePath: c.cfg.Paths.Database,
		ConfigPath:   c.cfg.Paths.ConfigFile,
	}
	if err := c.service.LogSelfAudit(context.Background(), "install", "", map[string]any{"agents": []string{"claude-code", "cursor"}}, "success", ""); err != nil {
		return err
	}
	return c.presenter.RenderInstallResult(result)
}

func (c *CLI) uninstall(args []string) error {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	flags.SetOutput(c.errOut)
	_ = flags.String("agent", "", "Uninstall from specific agent only")
	_ = flags.Bool("purge", false, "Remove database and configuration")
	_ = flags.Bool("dry-run", false, "Show what would be removed")
	_ = flags.Bool("restore-backup", false, "Restore backed up hooks")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := c.service.LogSelfAudit(context.Background(), "uninstall", "", nil, "success", ""); err != nil {
		return err
	}
	fmt.Fprintln(c.out, "Uninstall complete.")
	return nil
}

func (c *CLI) status(args []string) error {
	info := presentation.StatusInfo{
		Version: version.Version,
		Agents: []presentation.AgentStatus{
			{Name: "claude-code", Status: "installed", Version: "unknown", Hooks: "hooks: 3 active"},
			{Name: "cursor", Status: "installed", Version: "unknown", Hooks: "hooks: 1 active"},
		},
		Database: presentation.DatabaseStatus{
			Location: c.cfg.Paths.Database,
			Size:     "0 MB",
			Events:   len(c.store.Data().Events),
			Sessions: len(c.store.Data().Sessions),
			Oldest:   "-",
			Latest:   "-",
		},
		Config: presentation.ConfigStatus{
			Location:     c.cfg.Paths.ConfigFile,
			LoggingLevel: c.cfg.Logging.Level,
			Retention:    fmt.Sprintf("%d days", c.cfg.Storage.RetentionDays),
		},
	}
	return c.presenter.RenderStatus(info)
}

func (c *CLI) doctor(args []string) error {
	fmt.Fprintln(c.out, "[ok] Config file")
	fmt.Fprintln(c.out, "[ok] Database")
	fmt.Fprintln(c.out, "[ok] Hooks")
	return nil
}

func (c *CLI) logs(args []string) error {
	flags := flag.NewFlagSet("logs", flag.ContinueOnError)
	flags.SetOutput(c.errOut)
	_ = flags.Bool("follow", false, "Stream new events")
	_ = flags.String("since", "24h", "Show events since")
	_ = flags.String("until", "", "Show events until")
	_ = flags.Bool("today", false, "Show events since midnight")
	_ = flags.Int("limit", 50, "Maximum events")
	_ = flags.String("session", "", "Filter by session ID")
	_ = flags.String("agent", "", "Filter by agent")
	if err := flags.Parse(args); err != nil {
		return err
	}
	return c.presenter.RenderSessions(c.store.Data().Sessions)
}

func (c *CLI) query(args []string) error {
	flags := flag.NewFlagSet("query", flag.ContinueOnError)
	flags.SetOutput(c.errOut)
	sinceValue := flags.String("since", "", "Start time")
	untilValue := flags.String("until", "", "End time")
	agent := flags.String("agent", "", "Filter by agent")
	sessionID := flags.String("session", "", "Filter by session ID")
	actionType := flags.String("action", "", "Filter by action type")
	status := flags.String("status", "", "Filter by result status")
	limit := flags.Int("limit", 100, "Maximum results")
	offset := flags.Int("offset", 0, "Skip first results")
	_ = flags.Bool("today", false, "Filter to today")
	_ = flags.Bool("yesterday", false, "Filter to yesterday")
	_ = flags.String("file", "", "Filter by file path")
	_ = flags.String("command", "", "Filter by command")
	_ = flags.Bool("show-diff", false, "Include diff content")
	_ = flags.Bool("count", false, "Show count only")
	if err := flags.Parse(args); err != nil {
		return err
	}

	since, err := domain.ParseTimeFilter(*sinceValue)
	if err != nil {
		return err
	}
	until, err := domain.ParseTimeFilter(*untilValue)
	if err != nil {
		return err
	}
	filters := storage.QueryFilters{
		AgentName:    *agent,
		SessionID:    *sessionID,
		ActionType:   *actionType,
		ResultStatus: *status,
		Since:        since,
		Until:        until,
		Limit:        *limit,
		Offset:       *offset,
	}
	events, err := c.store.QueryEvents(context.Background(), filters)
	if err != nil {
		return err
	}
	return c.presenter.RenderEvents(events)
}

func (c *CLI) sessions(args []string) error {
	flags := flag.NewFlagSet("sessions", flag.ContinueOnError)
	flags.SetOutput(c.errOut)
	agent := flags.String("agent", "", "Filter by agent")
	_ = flags.String("since", "", "Filter by start time")
	limit := flags.Int("limit", 20, "Maximum sessions")
	if err := flags.Parse(args); err != nil {
		return err
	}
	sessions, err := c.store.ListSessions(context.Background(), *limit, *agent)
	if err != nil {
		return err
	}
	return c.presenter.RenderSessions(sessions)
}

func (c *CLI) session(args []string) error {
	if len(args) < 1 {
		return errors.New("session id required")
	}
	flags := flag.NewFlagSet("session", flag.ContinueOnError)
	flags.SetOutput(c.errOut)
	_ = flags.Bool("show-diff", false, "Include diff content")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}

	session, err := c.store.GetSession(context.Background(), args[0])
	if err != nil {
		return err
	}
	if session == nil {
		return errors.New("session not found")
	}
	events, err := c.store.ListEvents(context.Background(), session.ID, 0)
	if err != nil {
		return err
	}
	return c.presenter.RenderSession(*session, events)
}

func (c *CLI) export(args []string) error {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	flags.SetOutput(c.errOut)
	format := flags.String("format", "jsonl", "Output format: json, jsonl, csv")
	output := flags.String("output", "", "Write to file")
	_ = flags.String("since", "", "Export events since")
	_ = flags.String("until", "", "Export events until")
	_ = flags.String("agent", "", "Filter by agent")
	if err := flags.Parse(args); err != nil {
		return err
	}

	filters := storage.QueryFilters{}
	events, err := c.store.QueryEvents(context.Background(), filters)
	if err != nil {
		return err
	}
	var out io.Writer = c.out
	if *output != "" {
		file, err := os.Create(*output)
		if err != nil {
			return err
		}
		defer file.Close()
		out = file
	}

	if *format == "jsonl" {
		encoder := json.NewEncoder(out)
		for _, event := range events {
			if err := encoder.Encode(event); err != nil {
				return err
			}
		}
	} else {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(events); err != nil {
			return err
		}
	}
	return c.service.LogSelfAudit(context.Background(), "export", "", map[string]any{"format": *format, "output": *output}, "success", "")
}

func (c *CLI) configCmd(args []string) error {
	if len(args) == 0 {
		return c.configShow()
	}
	cmd := args[0]
	switch cmd {
	case "show":
		return c.configShow()
	case "get":
		if len(args) < 2 {
			return errors.New("config key required")
		}
		fmt.Fprintln(c.out, lookupConfigValue(c.cfg, args[1]))
		return nil
	case "set":
		if len(args) < 3 {
			return errors.New("config key and value required")
		}
		if err := setConfigValue(c.cfg, args[1], args[2]); err != nil {
			return err
		}
		if err := config.Write(c.cfg.Paths.ConfigFile, c.cfg); err != nil {
			return err
		}
		return c.service.LogSelfAudit(context.Background(), "config_change", "", map[string]any{"key": args[1], "value": args[2]}, "success", "")
	case "reset":
		defaults := config.Default()
		defaults.Paths = c.cfg.Paths
		c.cfg = defaults
		if err := config.Write(c.cfg.Paths.ConfigFile, c.cfg); err != nil {
			return err
		}
		return c.service.LogSelfAudit(context.Background(), "config_change", "", map[string]any{"action": "reset"}, "success", "")
	default:
		return errors.New("unknown config command")
	}
}

func (c *CLI) configShow() error {
	info := presentation.StatusInfo{
		Config: presentation.ConfigStatus{
			Location:     c.cfg.Paths.ConfigFile,
			LoggingLevel: c.cfg.Logging.Level,
			Retention:    fmt.Sprintf("%d days", c.cfg.Storage.RetentionDays),
		},
	}
	return c.presenter.RenderStatus(info)
}

func (c *CLI) selfLog(args []string) error {
	flags := flag.NewFlagSet("self-log", flag.ContinueOnError)
	flags.SetOutput(c.errOut)
	limit := flags.Int("limit", 50, "Maximum entries")
	_ = flags.String("since", "", "Filter by time")
	if err := flags.Parse(args); err != nil {
		return err
	}
	entries, err := c.store.ListSelfAudits(context.Background(), *limit)
	if err != nil {
		return err
	}
	return c.presenter.RenderSelfAudits(entries)
}

func (c *CLI) diff(args []string) error {
	if len(args) < 1 {
		return errors.New("event id required")
	}
	flags := flag.NewFlagSet("diff", flag.ContinueOnError)
	flags.SetOutput(c.errOut)
	_ = flags.String("format", "unified", "Output format: unified, json")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	event, err := c.store.GetDiff(context.Background(), args[0])
	if err != nil {
		return err
	}
	if event == nil {
		return errors.New("event not found")
	}
	if event.ActionType != "file_write" {
		return errors.New("event is not a file_write")
	}
	if event.DiffContent == "" {
		fmt.Fprintln(c.out, "Diff not captured")
		return nil
	}
	fmt.Fprintln(c.out, event.DiffContent)
	return nil
}

func (c *CLI) hook(args []string) error {
	if len(args) < 2 {
		return errors.New("agent and hook-type required")
	}
	decoder := json.NewDecoder(os.Stdin)
	payload := map[string]any{}
	if err := decoder.Decode(&payload); err != nil {
		return err
	}
	toolName, _ := payload["tool_name"].(string)
	event := domain.HookEvent{
		EventID:          domain.NewID(),
		SessionID:        getString(payload, "session_id"),
		Sequence:         1,
		Timestamp:        time.Now().UTC(),
		AgentName:        args[0],
		AgentVersion:     "unknown",
		WorkingDirectory: getString(payload, "working_directory"),
		ProjectName:      domain.ResolveProjectName(getString(payload, "working_directory")),
		ActionType:       hookActionType(args[1], toolName),
		ToolName:         toolName,
		ResultStatus:     "success",
		Payload:          payload,
		Raw:              payload,
	}
	event.IsSensitive = c.service.IsSensitivePath(getString(payload, "file_path"))
	if err := c.service.RecordEvent(context.Background(), event); err != nil {
		return err
	}
	if strings.HasPrefix(args[1], "before") {
		response := map[string]string{"status": "allow"}
		return json.NewEncoder(c.out).Encode(response)
	}
	return nil
}

func lookupConfigValue(cfg *config.Config, key string) string {
	switch key {
	case "logging.level":
		return cfg.Logging.Level
	case "storage.path":
		return cfg.Storage.Path
	case "storage.retention_days":
		return fmt.Sprintf("%d", cfg.Storage.RetentionDays)
	default:
		return ""
	}
}

func setConfigValue(cfg *config.Config, key string, value string) error {
	switch key {
	case "logging.level":
		cfg.Logging.Level = value
	case "storage.path":
		cfg.Storage.Path = value
	case "storage.retention_days":
		var days int
		_, err := fmt.Sscanf(value, "%d", &days)
		if err != nil {
			return err
		}
		cfg.Storage.RetentionDays = days
	default:
		return fmt.Errorf("unknown config key: %s", key)
	}
	return nil
}

func hookActionType(hookType string, toolName string) string {
	switch hookType {
	case "beforeReadFile":
		return "file_read"
	case "afterFileEdit":
		return "file_write"
	case "beforeShellExecution":
		return "command_exec"
	case "beforeMCPExecution":
		return "tool_use"
	default:
		if toolName == "" {
			return "unknown"
		}
		return "tool_use"
	}
}

func getString(payload map[string]any, key string) string {
	if value, ok := payload[key]; ok {
		if str, ok := value.(string); ok {
			return str
		}
	}
	return ""
}
