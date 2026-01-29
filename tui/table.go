package tui

import (
	"fmt"
	"io"
	"strings"
)

// TablePresenter renders output in table format.
type TablePresenter struct {
	w         io.Writer
	color     *Colorizer
	termWidth int
}

// NewTablePresenter creates a new table presenter.
func NewTablePresenter(opts PresenterOptions) *TablePresenter {
	termWidth := opts.TerminalWidth
	if termWidth == 0 {
		termWidth = GetTerminalWidth()
	}
	return &TablePresenter{
		w:         opts.Writer,
		color:     NewColorizer(opts.UseColors),
		termWidth: termWidth,
	}
}

// RenderStatus renders the tool status.
func (p *TablePresenter) RenderStatus(status *StatusView) error {
	fmt.Fprintf(p.w, "%s\n\n", p.color.Header("gryph "+status.Version))

	// Agents section
	fmt.Fprintf(p.w, "%s\n", p.color.Header("Agents"))
	for _, agent := range status.Agents {
		statusStr := p.color.StatusSkip()
		versionStr := "-"
		hooksStr := "-"

		if agent.Installed {
			statusStr = "installed"
			versionStr = agent.Version
			if agent.HooksActive {
				hooksStr = fmt.Sprintf("hooks: %d active", agent.HooksCount)
			} else {
				hooksStr = "hooks: not active"
			}
		} else {
			statusStr = "not found"
		}

		fmt.Fprintf(p.w, "  %-14s %-12s %-12s %s\n",
			p.color.Agent(agent.Name), statusStr, versionStr, hooksStr)
	}
	fmt.Fprintln(p.w)

	// Database section
	fmt.Fprintf(p.w, "%s\n", p.color.Header("Database"))
	fmt.Fprintf(p.w, "  %-14s %s\n", "Location", p.color.Path(status.Database.Location))
	fmt.Fprintf(p.w, "  %-14s %s\n", "Size", status.Database.SizeHuman)
	fmt.Fprintf(p.w, "  %-14s %s\n", "Events", p.color.Number(FormatNumber(status.Database.EventCount)))
	fmt.Fprintf(p.w, "  %-14s %s\n", "Sessions", p.color.Number(FormatNumber(status.Database.SessionCount)))
	if !status.Database.OldestEvent.IsZero() {
		fmt.Fprintf(p.w, "  %-14s %s\n", "Oldest", FormatTime(status.Database.OldestEvent))
		fmt.Fprintf(p.w, "  %-14s %s\n", "Latest", FormatTime(status.Database.NewestEvent))
	}
	fmt.Fprintln(p.w)

	// Config section
	fmt.Fprintf(p.w, "%s\n", p.color.Header("Config"))
	fmt.Fprintf(p.w, "  %-14s %s\n", "Location", p.color.Path(status.Config.Location))
	fmt.Fprintf(p.w, "  %-14s %s\n", "Logging level", status.Config.LoggingLevel)
	fmt.Fprintf(p.w, "  %-14s %d days\n", "Retention", status.Config.RetentionDays)

	return nil
}

// RenderSessions renders a list of sessions.
func (p *TablePresenter) RenderSessions(sessions []*SessionView) error {
	if len(sessions) == 0 {
		fmt.Fprintln(p.w, "No sessions found.")
		return nil
	}

	fmt.Fprintf(p.w, "Sessions (%d)\n", len(sessions))
	fmt.Fprintln(p.w, HorizontalLine(p.termWidth))

	for _, s := range sessions {
		fmt.Fprintf(p.w, "%s  %s  session %s\n",
			FormatTimeShort(s.StartedAt),
			p.color.Agent(s.AgentName),
			p.color.Dim(s.ShortID))

		summary := fmt.Sprintf("   %d actions", s.TotalActions)
		if s.FilesWritten > 0 {
			summary += fmt.Sprintf("  *  %d files written", s.FilesWritten)
		}
		if s.CommandsExecuted > 0 {
			summary += fmt.Sprintf("  *  %d commands", s.CommandsExecuted)
		}
		fmt.Fprintln(p.w, p.color.Dim(summary))
		fmt.Fprintln(p.w)
	}

	return nil
}

// RenderSession renders a single session detail.
func (p *TablePresenter) RenderSession(session *SessionView, events []*EventView) error {
	fmt.Fprintf(p.w, "%s\n", p.color.Header("Session Details"))
	fmt.Fprintln(p.w, HorizontalLine(p.termWidth))
	fmt.Fprintln(p.w)

	fmt.Fprintf(p.w, "%-16s %s\n", "Session ID", session.ID)
	fmt.Fprintf(p.w, "%-16s %s %s\n", "Agent", p.color.Agent(session.AgentDisplayName), session.AgentVersion)
	fmt.Fprintf(p.w, "%-16s %s\n", "Started", FormatTime(session.StartedAt))
	if !session.EndedAt.IsZero() {
		fmt.Fprintf(p.w, "%-16s %s\n", "Duration", FormatDuration(session.Duration))
	}
	if session.WorkingDirectory != "" {
		fmt.Fprintf(p.w, "%-16s %s\n", "Working Dir", p.color.Path(session.WorkingDirectory))
	}
	if session.ProjectName != "" {
		fmt.Fprintf(p.w, "%-16s %s\n", "Project", session.ProjectName)
	}
	fmt.Fprintln(p.w)

	if len(events) > 0 {
		fmt.Fprintf(p.w, "%s\n", p.color.Header("Actions"))
		fmt.Fprintln(p.w, HorizontalLine(p.termWidth))
		fmt.Fprintln(p.w)

		for i, e := range events {
			fmt.Fprintf(p.w, "#%-2d %s  %s\n", i+1, FormatTimeShort(e.Timestamp), e.ActionDisplay)
			if e.Path != "" {
				fmt.Fprintf(p.w, "    Path: %s\n", p.color.Path(e.Path))
			}
			if e.Command != "" {
				fmt.Fprintf(p.w, "    Command: %s\n", e.Command)
			}
			if e.LinesAdded > 0 || e.LinesRemoved > 0 {
				fmt.Fprintf(p.w, "    Changes: %s\n", FormatLineChanges(e.LinesAdded, e.LinesRemoved))
			}
			if e.ExitCode != 0 || e.ActionType == "command_exec" {
				fmt.Fprintf(p.w, "    Exit: %d\n", e.ExitCode)
			}
			fmt.Fprintln(p.w)
		}
	}

	// Summary line
	fmt.Fprintln(p.w, HorizontalLine(p.termWidth))
	summary := fmt.Sprintf("Summary: %d files read, %d files written, %d commands",
		session.FilesRead, session.FilesWritten, session.CommandsExecuted)
	if session.Errors > 0 {
		summary += fmt.Sprintf(" (%d errors)", session.Errors)
	}
	fmt.Fprintln(p.w, summary)

	return nil
}

// eventsColumnWidths holds the calculated widths for events table columns.
type eventsColumnWidths struct {
	time    int
	agent   int
	session int
	action  int
	path    int
	result  int
	total   int
}

// calculateEventsColumnWidths computes column widths based on terminal width.
// Fixed columns: Time(11), Agent(12), Session(9), Action(6), Result(10)
// Flexible column: Path (absorbs remaining space)
func (p *TablePresenter) calculateEventsColumnWidths() eventsColumnWidths {
	const (
		timeWidth    = 11
		agentWidth   = 12
		sessionWidth = 9
		actionWidth  = 6
		resultWidth  = 10
		minPathWidth = 15
		maxPathWidth = 80
		spacing      = 5 // spaces between columns
	)

	fixedWidth := timeWidth + agentWidth + sessionWidth + actionWidth + resultWidth + spacing
	availableForPath := p.termWidth - fixedWidth

	pathWidth := availableForPath
	if pathWidth < minPathWidth {
		pathWidth = minPathWidth
	}
	if pathWidth > maxPathWidth {
		pathWidth = maxPathWidth
	}

	totalWidth := fixedWidth + pathWidth

	return eventsColumnWidths{
		time:    timeWidth,
		agent:   agentWidth,
		session: sessionWidth,
		action:  actionWidth,
		path:    pathWidth,
		result:  resultWidth,
		total:   totalWidth,
	}
}

// RenderEvents renders a list of events.
func (p *TablePresenter) RenderEvents(events []*EventView) error {
	if len(events) == 0 {
		fmt.Fprintln(p.w, "No events found.")
		return nil
	}

	cols := p.calculateEventsColumnWidths()

	fmt.Fprintf(p.w, "Results (%d events)\n", len(events))
	fmt.Fprintln(p.w, HorizontalLine(cols.total))

	// Build format string dynamically
	headerFmt := fmt.Sprintf("%%-%ds %%-%ds %%-%ds %%-%ds %%-%ds %%s\n",
		cols.time, cols.agent, cols.session, cols.action, cols.path)
	fmt.Fprintf(p.w, headerFmt, "Time", "Agent", "Session", "Action", "Path/Command", "Result")
	fmt.Fprintln(p.w, HorizontalLine(cols.total))

	rowFmt := fmt.Sprintf("%%-%ds %%-%ds %%-%ds %%-%ds %%-%ds %%s\n",
		cols.time, cols.agent, cols.session, cols.action, cols.path)

	for _, e := range events {
		target := e.Path
		if target == "" {
			target = e.Command
		}
		target = TruncateString(target, cols.path)

		result := ""
		if e.LinesAdded > 0 || e.LinesRemoved > 0 {
			result = FormatLineChanges(e.LinesAdded, e.LinesRemoved)
		}
		if e.ExitCode != 0 {
			result = FormatExitCode(e.ExitCode)
		}

		fmt.Fprintf(p.w, rowFmt,
			FormatTimeShort(e.Timestamp),
			p.color.Agent(e.AgentName),
			e.ShortSessionID,
			e.ActionDisplay,
			target,
			result)
	}

	fmt.Fprintln(p.w, HorizontalLine(cols.total))
	fmt.Fprintf(p.w, "%d results\n", len(events))

	return nil
}

// RenderInstall renders the installation result.
func (p *TablePresenter) RenderInstall(result *InstallView) error {
	fmt.Fprintln(p.w, "Discovering agents...")
	fmt.Fprintln(p.w)

	for _, agent := range result.Agents {
		if agent.Installed {
			fmt.Fprintf(p.w, "  %s  %s %s\n", p.color.StatusOK(), agent.DisplayName, agent.Version)
			fmt.Fprintf(p.w, "        %s\n", p.color.Path(agent.Path))
		} else {
			fmt.Fprintf(p.w, "  %s  %s\n", p.color.StatusSkip(), agent.DisplayName)
			fmt.Fprintf(p.w, "        not installed\n")
		}
	}
	fmt.Fprintln(p.w)

	fmt.Fprintln(p.w, "Installing hooks...")
	fmt.Fprintln(p.w)

	for _, agent := range result.Agents {
		if !agent.Installed || len(agent.HooksInstalled) == 0 {
			continue
		}

		fmt.Fprintf(p.w, "  %s\n", agent.DisplayName)
		for _, hook := range agent.HooksInstalled {
			fmt.Fprintf(p.w, "    -> %-40s %s\n", hook+" hook", p.color.StatusOK())
		}
		for _, warning := range agent.Warnings {
			fmt.Fprintf(p.w, "    -> Note: %s\n", warning)
		}
		fmt.Fprintln(p.w)
	}

	fmt.Fprintln(p.w, "Installation complete.")
	fmt.Fprintln(p.w)
	fmt.Fprintf(p.w, "  %-11s %s\n", "Database", p.color.Path(result.Database))
	fmt.Fprintf(p.w, "  %-11s %s\n", "Config", p.color.Path(result.Config))
	fmt.Fprintln(p.w)
	fmt.Fprintln(p.w, "Run 'gryph status' to verify.")
	fmt.Fprintln(p.w, "Run 'gryph logs -f' to watch activity.")

	return nil
}

// RenderUninstall renders the uninstallation result.
func (p *TablePresenter) RenderUninstall(result *UninstallView) error {
	fmt.Fprintln(p.w, "Uninstalling hooks...")
	fmt.Fprintln(p.w)

	for _, agent := range result.Agents {
		if len(agent.HooksRemoved) == 0 {
			continue
		}

		fmt.Fprintf(p.w, "  %s\n", agent.DisplayName)
		for _, hook := range agent.HooksRemoved {
			fmt.Fprintf(p.w, "    -> Removed %s hook\n", hook)
		}
		if agent.BackupsRestored {
			fmt.Fprintf(p.w, "    -> Backups restored\n")
		}
		fmt.Fprintln(p.w)
	}

	fmt.Fprintln(p.w, "Uninstallation complete.")
	if result.Purged {
		fmt.Fprintln(p.w, "Database and config files have been removed.")
	}

	return nil
}

// RenderDoctor renders the doctor check results.
func (p *TablePresenter) RenderDoctor(result *DoctorView) error {
	fmt.Fprintf(p.w, "%s\n", p.color.Header("Doctor"))
	fmt.Fprintln(p.w, HorizontalLine(p.termWidth))
	fmt.Fprintln(p.w)

	for _, check := range result.Checks {
		var statusStr string
		switch check.Status {
		case CheckOK:
			statusStr = p.color.StatusOK()
		case CheckWarn:
			statusStr = p.color.Warning("[!!]")
		case CheckFail:
			statusStr = p.color.StatusFail()
		}

		fmt.Fprintf(p.w, "  %s  %s\n", statusStr, check.Name)
		if check.Message != "" {
			fmt.Fprintf(p.w, "        %s\n", check.Message)
		}
		if check.Suggestion != "" && check.Status != CheckOK {
			fmt.Fprintf(p.w, "        %s\n", p.color.Dim(check.Suggestion))
		}
	}
	fmt.Fprintln(p.w)

	if result.AllOK {
		fmt.Fprintln(p.w, p.color.Success("All checks passed."))
	} else {
		fmt.Fprintln(p.w, p.color.Warning("Some checks failed. See suggestions above."))
	}

	return nil
}

// RenderConfig renders the configuration.
func (p *TablePresenter) RenderConfig(config *ConfigView) error {
	fmt.Fprintf(p.w, "%s\n", p.color.Header("Configuration"))
	fmt.Fprintf(p.w, "Location: %s\n", p.color.Path(config.Location))
	fmt.Fprintln(p.w, HorizontalLine(p.termWidth))
	fmt.Fprintln(p.w)

	p.renderConfigMap(config.Values, "")

	return nil
}

func (p *TablePresenter) renderConfigMap(m map[string]interface{}, prefix string) {
	for key, value := range m {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		switch v := value.(type) {
		case map[string]interface{}:
			p.renderConfigMap(v, fullKey)
		default:
			fmt.Fprintf(p.w, "  %-30s %v\n", fullKey, value)
		}
	}
}

// RenderSelfAudits renders self-audit entries.
func (p *TablePresenter) RenderSelfAudits(entries []*SelfAuditView) error {
	if len(entries) == 0 {
		fmt.Fprintln(p.w, "No self-audit entries found.")
		return nil
	}

	fmt.Fprintf(p.w, "Self-Audit Log (%d entries)\n", len(entries))
	fmt.Fprintln(p.w, HorizontalLine(p.termWidth))

	for _, e := range entries {
		resultStr := p.color.Success(e.Result)
		if e.Result == "error" {
			resultStr = p.color.Error(e.Result)
		} else if e.Result == "skipped" {
			resultStr = p.color.Dim(e.Result)
		}

		fmt.Fprintf(p.w, "%s  %-18s %s\n",
			FormatTime(e.Timestamp), e.Action, resultStr)

		if e.AgentName != "" {
			fmt.Fprintf(p.w, "    Agent: %s\n", p.color.Agent(e.AgentName))
		}
		if e.ErrorMessage != "" {
			fmt.Fprintf(p.w, "    Error: %s\n", p.color.Error(e.ErrorMessage))
		}
	}

	return nil
}

// RenderDiff renders a diff view.
func (p *TablePresenter) RenderDiff(diff *DiffView) error {
	if !diff.Available {
		fmt.Fprintln(p.w, diff.Message)
		return nil
	}

	fmt.Fprintf(p.w, "%-10s %s\n", "Event:", diff.EventID)
	fmt.Fprintf(p.w, "%-10s %s\n", "Session:", diff.SessionID)
	fmt.Fprintf(p.w, "%-10s %s\n", "File:", p.color.Path(diff.FilePath))
	fmt.Fprintf(p.w, "%-10s %s\n", "Time:", FormatTime(diff.Timestamp))
	fmt.Fprintln(p.w)

	// Render diff content with colors
	for _, line := range strings.Split(diff.Content, "\n") {
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			fmt.Fprintln(p.w, p.color.DiffHeader(line))
		} else if strings.HasPrefix(line, "+") {
			fmt.Fprintln(p.w, p.color.DiffAdd(line))
		} else if strings.HasPrefix(line, "-") {
			fmt.Fprintln(p.w, p.color.DiffRemove(line))
		} else if strings.HasPrefix(line, "@@") {
			fmt.Fprintln(p.w, p.color.Cyan(line))
		} else {
			fmt.Fprintln(p.w, line)
		}
	}

	return nil
}

// RenderError renders an error message.
func (p *TablePresenter) RenderError(err error) error {
	fmt.Fprintf(p.w, "%s %s\n", p.color.Error("Error:"), err.Error())
	return nil
}

// RenderMessage renders a simple message.
func (p *TablePresenter) RenderMessage(message string) error {
	fmt.Fprintln(p.w, message)
	return nil
}

// Ensure TablePresenter implements Presenter
var _ Presenter = (*TablePresenter)(nil)
