package presentation

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/safedep/gryph/internal/storage"
)

type Presenter interface {
	RenderStatus(StatusInfo) error
	RenderSessions([]storage.Session) error
	RenderSession(storage.Session, []storage.AuditEvent) error
	RenderEvents([]storage.AuditEvent) error
	RenderSelfAudits([]storage.SelfAudit) error
	RenderInstallResult(InstallResult) error
	RenderError(error) error
}

type StatusInfo struct {
	Version      string
	Agents       []AgentStatus
	Database     DatabaseStatus
	Config       ConfigStatus
	ConfigPath   string
	DatabasePath string
}

type AgentStatus struct {
	Name    string
	Status  string
	Version string
	Hooks   string
}

type DatabaseStatus struct {
	Location string
	Size     string
	Events   int
	Sessions int
	Oldest   string
	Latest   string
}

type ConfigStatus struct {
	Location     string
	LoggingLevel string
	Retention    string
}

type InstallResult struct {
	Agents       []InstallAgentResult
	DatabasePath string
	ConfigPath   string
}

type InstallAgentResult struct {
	Name   string
	Status string
	Note   string
	Hooks  []string
}

type TablePresenter struct {
	Writer io.Writer
}

func NewTablePresenter(w io.Writer) *TablePresenter {
	return &TablePresenter{Writer: w}
}

func (p *TablePresenter) RenderStatus(info StatusInfo) error {
	fmt.Fprintf(p.Writer, "gryph %s\n\n", info.Version)
	fmt.Fprintln(p.Writer, "Agents")
	for _, agent := range info.Agents {
		fmt.Fprintf(p.Writer, "  %-12s %-11s %-10s %s\n", agent.Name, agent.Status, agent.Version, agent.Hooks)
	}
	fmt.Fprintln(p.Writer, "\nDatabase")
	fmt.Fprintf(p.Writer, "  Location       %s\n", info.Database.Location)
	fmt.Fprintf(p.Writer, "  Size           %s\n", info.Database.Size)
	fmt.Fprintf(p.Writer, "  Events         %d\n", info.Database.Events)
	fmt.Fprintf(p.Writer, "  Sessions       %d\n", info.Database.Sessions)
	fmt.Fprintf(p.Writer, "  Oldest         %s\n", info.Database.Oldest)
	fmt.Fprintf(p.Writer, "  Latest         %s\n", info.Database.Latest)
	fmt.Fprintln(p.Writer, "\nConfig")
	fmt.Fprintf(p.Writer, "  Location       %s\n", info.Config.Location)
	fmt.Fprintf(p.Writer, "  Logging level  %s\n", info.Config.LoggingLevel)
	fmt.Fprintf(p.Writer, "  Retention      %s\n", info.Config.Retention)
	return nil
}

func (p *TablePresenter) RenderSessions(sessions []storage.Session) error {
	fmt.Fprintln(p.Writer, "Sessions")
	fmt.Fprintln(p.Writer, "────────────────────────────────────────────────────────────────────")
	for _, session := range sessions {
		fmt.Fprintf(p.Writer, "%s  %s  session %s\n", session.StartedAt.Format("15:04"), session.AgentName, session.ID)
	}
	return nil
}

func (p *TablePresenter) RenderSession(session storage.Session, events []storage.AuditEvent) error {
	fmt.Fprintln(p.Writer, "Session Details")
	fmt.Fprintln(p.Writer, "────────────────────────────────────────────────────────────────────")
	fmt.Fprintf(p.Writer, "Session ID      %s\n", session.ID)
	fmt.Fprintf(p.Writer, "Agent           %s %s\n", session.AgentName, session.AgentVersion)
	fmt.Fprintf(p.Writer, "Started         %s\n", session.StartedAt.Format(time.RFC3339))
	if session.EndedAt != nil {
		fmt.Fprintf(p.Writer, "Ended           %s\n", session.EndedAt.Format(time.RFC3339))
	}
	fmt.Fprintf(p.Writer, "Working Dir     %s\n", session.WorkingDirectory)
	fmt.Fprintf(p.Writer, "Project         %s\n\n", session.ProjectName)
	fmt.Fprintln(p.Writer, "Actions")
	fmt.Fprintln(p.Writer, "────────────────────────────────────────────────────────────────────")
	for _, event := range events {
		fmt.Fprintf(p.Writer, "#%d  %s  %s\n", event.Sequence, event.Timestamp.Format("15:04:05"), event.ActionType)
		if len(event.Payload) > 0 {
			fmt.Fprintf(p.Writer, "    Payload: %s\n", strings.TrimSpace(string(event.Payload)))
		}
		if event.ErrorMessage != "" {
			fmt.Fprintf(p.Writer, "    Error: %s\n", event.ErrorMessage)
		}
		fmt.Fprintln(p.Writer)
	}
	return nil
}

func (p *TablePresenter) RenderEvents(events []storage.AuditEvent) error {
	fmt.Fprintln(p.Writer, "Results")
	fmt.Fprintln(p.Writer, "────────────────────────────────────────────────────────────────────")
	for _, event := range events {
		fmt.Fprintf(p.Writer, "%s  %-10s  %-8s  %s\n", event.Timestamp.Format("15:04:05"), event.AgentName, event.ActionType, event.ResultStatus)
	}
	return nil
}

func (p *TablePresenter) RenderSelfAudits(audits []storage.SelfAudit) error {
	fmt.Fprintln(p.Writer, "Self Audit")
	fmt.Fprintln(p.Writer, "────────────────────────────────────────────────────────────────────")
	for _, audit := range audits {
		fmt.Fprintf(p.Writer, "%s  %-12s  %s\n", audit.Timestamp.Format("2006-01-02 15:04:05"), audit.Action, audit.Result)
	}
	return nil
}

func (p *TablePresenter) RenderInstallResult(result InstallResult) error {
	fmt.Fprintln(p.Writer, "Discovering agents...\n")
	for _, agent := range result.Agents {
		fmt.Fprintf(p.Writer, "  [%s]  %s\n", agent.Status, agent.Name)
		if agent.Note != "" {
			fmt.Fprintf(p.Writer, "        %s\n", agent.Note)
		}
	}
	fmt.Fprintln(p.Writer, "\nInstallation complete.\n")
	fmt.Fprintf(p.Writer, "  Database    %s\n", result.DatabasePath)
	fmt.Fprintf(p.Writer, "  Config      %s\n", result.ConfigPath)
	return nil
}

func (p *TablePresenter) RenderError(err error) error {
	fmt.Fprintf(p.Writer, "Error: %s\n", err)
	return nil
}

type JSONPresenter struct {
	Writer io.Writer
}

func NewJSONPresenter(w io.Writer) *JSONPresenter {
	return &JSONPresenter{Writer: w}
}

func (p *JSONPresenter) RenderStatus(info StatusInfo) error {
	return encodeJSON(p.Writer, info)
}

func (p *JSONPresenter) RenderSessions(sessions []storage.Session) error {
	return encodeJSON(p.Writer, sessions)
}

func (p *JSONPresenter) RenderSession(session storage.Session, events []storage.AuditEvent) error {
	payload := map[string]any{"session": session, "events": events}
	return encodeJSON(p.Writer, payload)
}

func (p *JSONPresenter) RenderEvents(events []storage.AuditEvent) error {
	return encodeJSON(p.Writer, events)
}

func (p *JSONPresenter) RenderSelfAudits(audits []storage.SelfAudit) error {
	return encodeJSON(p.Writer, audits)
}

func (p *JSONPresenter) RenderInstallResult(result InstallResult) error {
	return encodeJSON(p.Writer, result)
}

func (p *JSONPresenter) RenderError(err error) error {
	return encodeJSON(p.Writer, map[string]string{"error": err.Error()})
}

func encodeJSON(w io.Writer, payload any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}
