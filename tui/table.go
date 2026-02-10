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
	tw := &tableWriter{w: p.w}

	tw.printf("%s\n\n", p.color.Header("gryph "+status.Version))

	// Agents section
	tw.printf("%s\n", p.color.Header("Agents"))

	for _, agent := range status.Agents {
		statusStr := ""
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

		tw.printf("  %s %-12s %-12s %s\n",
			PadRightVisible(p.color.Agent(agent.Name), 14), statusStr, versionStr, hooksStr)
	}

	tw.println()

	// Database section
	tw.printf("%s\n", p.color.Header("Database"))
	tw.printf("  %-14s %s\n", "Location", p.color.Path(status.Database.Location))
	tw.printf("  %-14s %s\n", "Size", status.Database.SizeHuman)
	tw.printf("  %-14s %s\n", "Events", p.color.Number(FormatNumber(status.Database.EventCount)))
	tw.printf("  %-14s %s\n", "Sessions", p.color.Number(FormatNumber(status.Database.SessionCount)))

	if !status.Database.OldestEvent.IsZero() {
		tw.printf("  %-14s %s\n", "Oldest", FormatTime(status.Database.OldestEvent))
		tw.printf("  %-14s %s\n", "Latest", FormatTime(status.Database.NewestEvent))
	}
	tw.println()

	// Config section
	tw.printf("%s\n", p.color.Header("Config"))
	tw.printf("  %-14s %s\n", "Location", p.color.Path(status.Config.Location))
	tw.printf("  %-14s %s\n", "Logging level", status.Config.LoggingLevel)
	if status.Config.RetentionDays == 0 {
		tw.printf("  %-14s %s\n", "Retention", "disabled")
	} else {
		tw.printf("  %-14s %d days\n", "Retention", status.Config.RetentionDays)
		if status.Config.EventsToClean > 0 {
			tw.printf("  %-14s %d events ready for cleanup\n", "", status.Config.EventsToClean)
		}
	}

	return tw.Err()
}

// RenderSessions renders a list of sessions.
func (p *TablePresenter) RenderSessions(sessions []*SessionView) error {
	tw := &tableWriter{w: p.w}

	if len(sessions) == 0 {
		tw.println("No sessions found.")
		return tw.Err()
	}

	tw.printf("Sessions (%d)\n", len(sessions))
	tw.println(HorizontalLine(p.termWidth))

	for _, s := range sessions {
		tw.printf("%s  %s  session %s\n",
			FormatTime(s.StartedAt),
			p.color.Agent(s.AgentName),
			p.color.Dim(s.ShortID))

		summary := fmt.Sprintf("   %d actions", s.TotalActions)
		if s.FilesWritten > 0 {
			summary += fmt.Sprintf("  *  %d files written", s.FilesWritten)
		}
		if s.CommandsExecuted > 0 {
			summary += fmt.Sprintf("  *  %d commands", s.CommandsExecuted)
		}

		tw.println(p.color.Dim(summary))
		tw.println()
	}

	return tw.Err()
}

// RenderSession renders a single session detail.
func (p *TablePresenter) RenderSession(session *SessionView, events []*EventView) error {
	tw := &tableWriter{w: p.w}

	tw.printf("%s\n", p.color.Header("Session Details"))
	tw.println(HorizontalLine(p.termWidth))
	tw.println()

	tw.printf("%-16s %s\n", "Session ID", session.ID)
	tw.printf("%-16s %s %s\n", "Agent", p.color.Agent(session.AgentDisplayName), session.AgentVersion)
	tw.printf("%-16s %s\n", "Started", FormatTime(session.StartedAt))
	if !session.EndedAt.IsZero() {
		tw.printf("%-16s %s\n", "Duration", FormatDuration(session.Duration))
	}
	if session.WorkingDirectory != "" {
		tw.printf("%-16s %s\n", "Working Dir", p.color.Path(session.WorkingDirectory))
	}
	if session.ProjectName != "" {
		tw.printf("%-16s %s\n", "Project", session.ProjectName)
	}

	if len(events) > 0 {
		tw.printf("%s\n", p.color.Header("Actions"))
		tw.println(HorizontalLine(p.termWidth))
		tw.println()

		for i, e := range events {
			tw.printf("#%-2d %s  %s  %s\n", i+1, FormatTime(e.Timestamp), p.color.Dim(e.ShortID), e.ActionDisplay)
			if e.Path != "" {
				tw.printf("    Path: %s\n", p.color.Path(e.Path))
			}
			if e.Command != "" {
				tw.printf("    Command: %s\n", e.Command)
			}
			if e.ToolName != "" && e.Path == "" && e.Command == "" {
				tw.printf("    Tool: %s\n", e.ToolName)
			}
			if e.LinesAdded > 0 || e.LinesRemoved > 0 {
				tw.printf("    Changes: %s\n", FormatLineChanges(e.LinesAdded, e.LinesRemoved))
			}
			if e.ExitCode != 0 {
				tw.printf("    Exit: %s\n", FormatExitCode(e.ExitCode))
			} else if e.ActionType == "command_exec" {
				tw.printf("    Exit: 0\n")
			}
			if e.ResultStatus == "error" && e.ExitCode == 0 {
				tw.printf("    Status: error\n")
			}
			tw.println()
		}
	}

	// Summary line
	tw.println(HorizontalLine(p.termWidth))
	summary := fmt.Sprintf("Summary: %d files read, %d files written, %d commands",
		session.FilesRead, session.FilesWritten, session.CommandsExecuted)
	if session.Errors > 0 {
		summary += fmt.Sprintf(" (%d errors)", session.Errors)
	}
	tw.println(summary)

	return tw.Err()
}

// eventsColumnWidths holds the calculated widths for events table columns.
type eventsColumnWidths struct {
	time    int
	event   int
	agent   int
	session int
	action  int
	path    int
	result  int
	total   int
}

// calculateEventsColumnWidths computes column widths based on terminal width.
// Fixed columns: Time(11), Event(9), Agent(12), Session(9), Action(6), Result(10)
// Flexible column: Path (absorbs remaining space)
func (p *TablePresenter) calculateEventsColumnWidths() eventsColumnWidths {
	const (
		timeWidth    = 21
		eventWidth   = 9
		agentWidth   = 12
		sessionWidth = 9
		actionWidth  = 6
		resultWidth  = 10
		minPathWidth = 15
		maxPathWidth = 80
		spacing      = 6 // spaces between columns
	)

	fixedWidth := timeWidth + eventWidth + agentWidth + sessionWidth + actionWidth + resultWidth + spacing
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
		event:   eventWidth,
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
	tw := &tableWriter{w: p.w}

	if len(events) == 0 {
		tw.println("No events found.")
		return tw.Err()
	}

	cols := p.calculateEventsColumnWidths()

	tw.printf("Results (%d events)\n", len(events))
	tw.println(HorizontalLine(cols.total))

	tw.printf("%s %s %s %s %s %s %s\n",
		PadRightVisible("Time", cols.time),
		PadRightVisible("Event", cols.event),
		PadRightVisible("Agent", cols.agent),
		PadRightVisible("Session", cols.session),
		PadRightVisible("Action", cols.action),
		PadRightVisible("Path/Command", cols.path),
		"Result")
	tw.println(HorizontalLine(cols.total))

	for _, e := range events {
		target := e.Path
		if target == "" {
			target = e.Command
		}
		if target == "" {
			target = e.ToolName
		}
		target = TruncateString(target, cols.path)

		result := ""
		switch {
		case e.LinesAdded > 0 || e.LinesRemoved > 0:
			result = FormatLineChanges(e.LinesAdded, e.LinesRemoved)
		case e.ExitCode != 0:
			result = FormatExitCode(e.ExitCode)
		case e.ActionType == "command_exec":
			result = "exit:0"
		case e.ResultStatus == "error":
			result = "error"
		case e.ResultStatus == "blocked":
			result = "blocked"
		}

		tw.printf("%s %s %s %s %s %s %s\n",
			PadRightVisible(FormatTime(e.Timestamp), cols.time),
			PadRightVisible(e.ShortID, cols.event),
			PadRightVisible(p.color.Agent(e.AgentName), cols.agent),
			PadRightVisible(e.ShortSessionID, cols.session),
			PadRightVisible(e.ActionDisplay, cols.action),
			PadRightVisible(target, cols.path),
			result)
	}

	tw.println(HorizontalLine(cols.total))
	tw.printf("%d results\n", len(events))

	return tw.Err()
}

// RenderInstall renders the installation result.
func (p *TablePresenter) RenderInstall(result *InstallView) error {
	tw := &tableWriter{w: p.w}

	tw.println()

	for _, agent := range result.Agents {
		if agent.Installed {
			tw.printf("  %s  %s %s\n", p.color.StatusOK(), agent.DisplayName, agent.Version)
			tw.printf("        %s\n", p.color.Path(agent.Path))
		} else {
			tw.printf("  %s  %s\n", p.color.StatusSkip(), agent.DisplayName)
			tw.printf("        not installed\n")
		}
	}
	tw.println()

	tw.println("Installing hooks...")
	tw.println()

	for _, agent := range result.Agents {
		if !agent.Installed || len(agent.HooksInstalled) == 0 {
			continue
		}

		tw.printf("  %s\n", agent.DisplayName)
		for _, hook := range agent.HooksInstalled {
			tw.printf("    -> %-40s %s\n", hook+" hook", p.color.StatusOK())
		}
		for _, warning := range agent.Warnings {
			tw.printf("    -> Note: %s\n", warning)
		}
		tw.println()
	}

	tw.println("Installation complete.")
	tw.println()
	tw.printf("  %-11s %s\n", "Database", p.color.Path(result.Database))
	tw.printf("  %-11s %s\n", "Config", p.color.Path(result.Config))
	tw.println()
	tw.println("Run 'gryph status' to verify.")
	tw.println("Run 'gryph logs -f' to watch activity.")

	return tw.Err()
}

// RenderUninstall renders the uninstallation result.
func (p *TablePresenter) RenderUninstall(result *UninstallView) error {
	tw := &tableWriter{w: p.w}

	tw.println("Uninstalling hooks...")
	tw.println()

	for _, agent := range result.Agents {
		if len(agent.HooksRemoved) == 0 {
			continue
		}

		tw.printf("  %s\n", agent.DisplayName)
		for _, hook := range agent.HooksRemoved {
			tw.printf("    -> Removed %s hook\n", hook)
		}
		if agent.BackupsRestored {
			tw.printf("    -> Backups restored\n")
		}
		tw.println()
	}

	tw.println("Uninstallation complete.")
	if result.Purged {
		tw.println("Database and config files have been removed.")
	}

	return tw.Err()
}

// RenderDoctor renders the doctor check results.
func (p *TablePresenter) RenderDoctor(result *DoctorView) error {
	tw := &tableWriter{w: p.w}

	tw.printf("%s\n", p.color.Header("Doctor"))
	tw.println(HorizontalLine(p.termWidth))
	tw.println()

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

		tw.printf("  %s  %s\n", statusStr, check.Name)
		if check.Message != "" {
			tw.printf("        %s\n", check.Message)
		}
		if check.Suggestion != "" && check.Status != CheckOK {
			tw.printf("        %s\n", p.color.Dim(check.Suggestion))
		}
	}
	tw.println()

	if result.AllOK {
		tw.println(p.color.Success("All checks passed."))
	} else {
		tw.println(p.color.Warning("Some checks failed. See suggestions above."))
	}

	return tw.Err()
}

// RenderConfig renders the configuration.
func (p *TablePresenter) RenderConfig(config *ConfigView) error {
	tw := &tableWriter{w: p.w}

	tw.printf("%s\n", p.color.Header("Configuration"))
	tw.printf("Location: %s\n", p.color.Path(config.Location))
	tw.println(HorizontalLine(p.termWidth))
	tw.println()

	p.renderConfigMap(tw, config.Values, "")

	return tw.Err()
}

func (p *TablePresenter) renderConfigMap(tw *tableWriter, m map[string]interface{}, prefix string) {
	for key, value := range m {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		switch v := value.(type) {
		case map[string]interface{}:
			p.renderConfigMap(tw, v, fullKey)
		default:
			tw.printf("  %-30s %v\n", fullKey, value)
		}
	}
}

// RenderSelfAudits renders self-audit entries.
func (p *TablePresenter) RenderSelfAudits(entries []*SelfAuditView) error {
	tw := &tableWriter{w: p.w}

	if len(entries) == 0 {
		tw.println("No self-audit entries found.")
		return tw.Err()
	}

	tw.printf("Self-Audit Log (%d entries)\n", len(entries))
	tw.println(HorizontalLine(p.termWidth))

	for _, e := range entries {
		var resultStr string
		switch e.Result {
		case "error":
			resultStr = p.color.Error(e.Result)
		case "skipped":
			resultStr = p.color.Dim(e.Result)
		default:
			resultStr = p.color.Success(e.Result)
		}

		tw.printf("%s  %-18s %s\n", FormatTime(e.Timestamp), e.Action, resultStr)

		if e.AgentName != "" {
			tw.printf("    Agent: %s\n", p.color.Agent(e.AgentName))
		}
		if e.ErrorMessage != "" {
			tw.printf("    Error: %s\n", p.color.Error(e.ErrorMessage))
		}
	}

	return tw.Err()
}

// RenderDiff renders a diff view.
func (p *TablePresenter) RenderDiff(diff *DiffView) error {
	tw := &tableWriter{w: p.w}

	if !diff.Available {
		tw.println(diff.Message)
		return tw.Err()
	}

	tw.printf("%-10s %s\n", "Event:", diff.EventID)
	tw.printf("%-10s %s\n", "Session:", diff.SessionID)
	tw.printf("%-10s %s\n", "File:", p.color.Path(diff.FilePath))
	tw.printf("%-10s %s\n", "Time:", FormatTime(diff.Timestamp))
	tw.println()

	// Render diff content with colors
	for _, line := range strings.Split(diff.Content, "\n") {
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			tw.println(p.color.DiffHeader(line))
		} else if strings.HasPrefix(line, "+") {
			tw.println(p.color.DiffAdd(line))
		} else if strings.HasPrefix(line, "-") {
			tw.println(p.color.DiffRemove(line))
		} else if strings.HasPrefix(line, "@@") {
			tw.println(p.color.Cyan(line))
		} else {
			tw.println(line)
		}
	}

	return tw.Err()
}

// RenderError renders an error message.
func (p *TablePresenter) RenderError(err error) error {
	tw := &tableWriter{w: p.w}
	tw.printf("%s %s\n", p.color.Error("Error:"), err.Error())
	return tw.Err()
}

// RenderMessage renders a simple message.
func (p *TablePresenter) RenderMessage(message string) error {
	tw := &tableWriter{w: p.w}
	tw.println(message)
	return tw.Err()
}

// RenderStreamSync renders stream sync results.
func (p *TablePresenter) RenderStreamSync(result *StreamSyncView) error {
	tw := &tableWriter{w: p.w}

	tw.printf("%s\n", p.color.Header("Stream Sync Complete"))
	tw.println(HorizontalLine(30))
	tw.println()

	for _, tr := range result.TargetResults {
		if tr.Error != "" {
			tw.printf("  %s  %-14s %s\n",
				p.color.StatusFail(),
				tr.TargetName,
				p.color.Error("error: "+tr.Error))
		} else {
			tw.printf("  %s  %-14s %d events, %d audits\n",
				p.color.StatusOK(),
				tr.TargetName,
				tr.EventsSent,
				tr.AuditsSent)
		}
	}

	tw.println()

	if result.HasErrors {
		errCount := 0
		for _, tr := range result.TargetResults {
			if tr.Error != "" {
				errCount++
			}
		}
		successCount := len(result.TargetResults) - errCount
		tw.printf("Synced %d events, %d audits to %d target(s) with %d error(s).\n",
			result.TotalEvents, result.TotalAudits, successCount, errCount)
	} else {
		tw.printf("Synced %d events, %d audits to %d target(s).\n",
			result.TotalEvents, result.TotalAudits, len(result.TargetResults))
	}

	return tw.Err()
}

// RenderUpdateNotice renders an update availability notice.
func (p *TablePresenter) RenderUpdateNotice(notice *UpdateNoticeView) error {
	tw := &tableWriter{w: p.w}
	tw.println()
	tw.printf("%s %s → %s\n",
		p.color.Warning("A new version of gryph is available:"),
		p.color.Dim(notice.CurrentVersion),
		p.color.Success(notice.LatestVersion))
	tw.printf("  %s\n", p.color.Dim(notice.ReleaseURL))
	return tw.Err()
}

// Ensure TablePresenter implements Presenter
var _ Presenter = (*TablePresenter)(nil)
