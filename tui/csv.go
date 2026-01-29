package tui

import (
	"encoding/csv"
	"fmt"
	"io"
)

// CSVPresenter renders output as CSV.
type CSVPresenter struct {
	w      io.Writer
	writer *csv.Writer
}

// NewCSVPresenter creates a new CSV presenter.
func NewCSVPresenter(opts PresenterOptions) *CSVPresenter {
	return &CSVPresenter{
		w:      opts.Writer,
		writer: csv.NewWriter(opts.Writer),
	}
}

// RenderStatus renders the tool status as CSV.
func (p *CSVPresenter) RenderStatus(status *StatusView) error {
	// Write header
	p.writer.Write([]string{"type", "name", "value"})

	// Version
	p.writer.Write([]string{"version", "gryph", status.Version})

	// Agents
	for _, agent := range status.Agents {
		installed := "false"
		if agent.Installed {
			installed = "true"
		}
		p.writer.Write([]string{"agent", agent.Name, installed})
		p.writer.Write([]string{"agent_version", agent.Name, agent.Version})
	}

	// Database
	p.writer.Write([]string{"database", "location", status.Database.Location})
	p.writer.Write([]string{"database", "events", fmt.Sprintf("%d", status.Database.EventCount)})
	p.writer.Write([]string{"database", "sessions", fmt.Sprintf("%d", status.Database.SessionCount)})

	p.writer.Flush()
	return p.writer.Error()
}

// RenderSessions renders a list of sessions as CSV.
func (p *CSVPresenter) RenderSessions(sessions []*SessionView) error {
	// Write header
	p.writer.Write([]string{
		"id", "agent", "started_at", "duration", "actions",
		"files_read", "files_written", "commands", "errors",
	})

	for _, s := range sessions {
		p.writer.Write([]string{
			s.ID,
			s.AgentName,
			FormatTime(s.StartedAt),
			FormatDuration(s.Duration),
			fmt.Sprintf("%d", s.TotalActions),
			fmt.Sprintf("%d", s.FilesRead),
			fmt.Sprintf("%d", s.FilesWritten),
			fmt.Sprintf("%d", s.CommandsExecuted),
			fmt.Sprintf("%d", s.Errors),
		})
	}

	p.writer.Flush()
	return p.writer.Error()
}

// RenderSession renders a single session detail as CSV.
func (p *CSVPresenter) RenderSession(session *SessionView, events []*EventView) error {
	// Render events
	return p.RenderEvents(events)
}

// RenderEvents renders a list of events as CSV.
func (p *CSVPresenter) RenderEvents(events []*EventView) error {
	// Write header
	p.writer.Write([]string{
		"id", "session_id", "timestamp", "agent", "action",
		"tool", "path", "command", "lines_added", "lines_removed",
		"exit_code", "status", "sensitive",
	})

	for _, e := range events {
		sensitive := "false"
		if e.IsSensitive {
			sensitive = "true"
		}
		p.writer.Write([]string{
			e.ID,
			e.SessionID,
			FormatTime(e.Timestamp),
			e.AgentName,
			e.ActionType,
			e.ToolName,
			e.Path,
			e.Command,
			fmt.Sprintf("%d", e.LinesAdded),
			fmt.Sprintf("%d", e.LinesRemoved),
			fmt.Sprintf("%d", e.ExitCode),
			e.ResultStatus,
			sensitive,
		})
	}

	p.writer.Flush()
	return p.writer.Error()
}

// RenderInstall renders the installation result as CSV.
func (p *CSVPresenter) RenderInstall(result *InstallView) error {
	p.writer.Write([]string{"agent", "installed", "version", "hooks"})

	for _, agent := range result.Agents {
		installed := "false"
		if agent.Installed {
			installed = "true"
		}
		hooks := ""
		for i, h := range agent.HooksInstalled {
			if i > 0 {
				hooks += ";"
			}
			hooks += h
		}
		p.writer.Write([]string{
			agent.Name,
			installed,
			agent.Version,
			hooks,
		})
	}

	p.writer.Flush()
	return p.writer.Error()
}

// RenderUninstall renders the uninstallation result as CSV.
func (p *CSVPresenter) RenderUninstall(result *UninstallView) error {
	p.writer.Write([]string{"agent", "hooks_removed", "backups_restored"})

	for _, agent := range result.Agents {
		restored := "false"
		if agent.BackupsRestored {
			restored = "true"
		}
		hooks := ""
		for i, h := range agent.HooksRemoved {
			if i > 0 {
				hooks += ";"
			}
			hooks += h
		}
		p.writer.Write([]string{
			agent.Name,
			hooks,
			restored,
		})
	}

	p.writer.Flush()
	return p.writer.Error()
}

// RenderDoctor renders the doctor check results as CSV.
func (p *CSVPresenter) RenderDoctor(result *DoctorView) error {
	p.writer.Write([]string{"check", "status", "message", "suggestion"})

	for _, check := range result.Checks {
		p.writer.Write([]string{
			check.Name,
			string(check.Status),
			check.Message,
			check.Suggestion,
		})
	}

	p.writer.Flush()
	return p.writer.Error()
}

// RenderConfig renders the configuration as CSV.
func (p *CSVPresenter) RenderConfig(config *ConfigView) error {
	p.writer.Write([]string{"key", "value"})
	p.renderConfigMap(config.Values, "")
	p.writer.Flush()
	return p.writer.Error()
}

func (p *CSVPresenter) renderConfigMap(m map[string]interface{}, prefix string) {
	for key, value := range m {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		switch v := value.(type) {
		case map[string]interface{}:
			p.renderConfigMap(v, fullKey)
		default:
			p.writer.Write([]string{fullKey, fmt.Sprintf("%v", value)})
		}
	}
}

// RenderSelfAudits renders self-audit entries as CSV.
func (p *CSVPresenter) RenderSelfAudits(entries []*SelfAuditView) error {
	p.writer.Write([]string{"id", "timestamp", "action", "agent", "result", "error", "version"})

	for _, e := range entries {
		p.writer.Write([]string{
			e.ID,
			FormatTime(e.Timestamp),
			e.Action,
			e.AgentName,
			e.Result,
			e.ErrorMessage,
			e.ToolVersion,
		})
	}

	p.writer.Flush()
	return p.writer.Error()
}

// RenderDiff renders a diff view as CSV (content as single field).
func (p *CSVPresenter) RenderDiff(diff *DiffView) error {
	p.writer.Write([]string{"event_id", "session_id", "file_path", "timestamp", "content"})

	content := diff.Content
	if !diff.Available {
		content = diff.Message
	}

	p.writer.Write([]string{
		diff.EventID,
		diff.SessionID,
		diff.FilePath,
		FormatTime(diff.Timestamp),
		content,
	})

	p.writer.Flush()
	return p.writer.Error()
}

// RenderError renders an error message as CSV.
func (p *CSVPresenter) RenderError(err error) error {
	p.writer.Write([]string{"error"})
	p.writer.Write([]string{err.Error()})
	p.writer.Flush()
	return p.writer.Error()
}

// RenderMessage renders a simple message as CSV.
func (p *CSVPresenter) RenderMessage(message string) error {
	p.writer.Write([]string{"message"})
	p.writer.Write([]string{message})
	p.writer.Flush()
	return p.writer.Error()
}

// Ensure CSVPresenter implements Presenter
var _ Presenter = (*CSVPresenter)(nil)
