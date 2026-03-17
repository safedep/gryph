package query

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/tui"
)

type sessionSummary struct {
	filesWritten   []fileSummary
	filesRead      int
	filesDeleted   int
	commands       []cmdSummary
	commandsFailed int
	sensitive      int
	blocked        int
	errors         int
}

type fileSummary struct {
	path         string
	linesAdded   int
	linesRemoved int
}

type cmdSummary struct {
	command  string
	exitCode int
}

func computeSummary(evts []*events.Event) sessionSummary {
	var s sessionSummary
	for _, e := range evts {
		switch e.ActionType {
		case events.ActionFileRead:
			s.filesRead++
		case events.ActionFileWrite:
			fs := fileSummary{}
			if p, err := e.GetFileWritePayload(); err == nil && p != nil {
				fs.path = p.Path
				fs.linesAdded = p.LinesAdded
				fs.linesRemoved = p.LinesRemoved
			}
			s.filesWritten = append(s.filesWritten, fs)
		case events.ActionFileDelete:
			s.filesDeleted++
		case events.ActionCommandExec:
			cs := cmdSummary{}
			if p, err := e.GetCommandExecPayload(); err == nil && p != nil {
				cs.command = p.Command
				cs.exitCode = p.ExitCode
				if p.ExitCode != 0 {
					s.commandsFailed++
				}
			}
			s.commands = append(s.commands, cs)
		}

		if e.IsSensitive {
			s.sensitive++
		}
		if e.ResultStatus == events.ResultBlocked || e.ResultStatus == events.ResultRejected {
			s.blocked++
		}
		if e.ResultStatus == events.ResultError {
			s.errors++
		}
	}
	return s
}

func (m Model) renderSummary(width int) string {
	s := m.summary
	var sb strings.Builder

	sb.WriteString(summaryLabelStyle.Render(" Summary") + "\n")
	sb.WriteString(dimStyle.Render(strings.Repeat("─", width)) + "\n")

	sb.WriteString(fmt.Sprintf("  %s  %s written  %s read  %s deleted\n",
		summaryLabelStyle.Render("Files"),
		summaryValueStyle.Render(fmt.Sprintf("%d", len(s.filesWritten))),
		summaryValueStyle.Render(fmt.Sprintf("%d", s.filesRead)),
		summaryValueStyle.Render(fmt.Sprintf("%d", s.filesDeleted)),
	))

	for _, f := range s.filesWritten {
		changes := tui.FormatLineChanges(f.linesAdded, f.linesRemoved)
		path := f.path
		maxPath := width - len(changes) - 10
		if maxPath > 0 && len(path) > maxPath {
			path = "..." + path[len(path)-maxPath+3:]
		}
		sb.WriteString(fmt.Sprintf("    %s %s  %s\n",
			addedStyle.Render("+"),
			path,
			dimStyle.Render(changes),
		))
	}

	sb.WriteString("\n")

	sb.WriteString(fmt.Sprintf("  %s  %s executed  %s failed\n",
		summaryLabelStyle.Render("Cmds"),
		summaryValueStyle.Render(fmt.Sprintf("%d", len(s.commands))),
		summaryValueStyle.Render(fmt.Sprintf("%d", s.commandsFailed)),
	))

	for _, c := range s.commands {
		exitStyle := dimStyle
		if c.exitCode != 0 {
			exitStyle = lipgloss.NewStyle().Foreground(colorRed)
		}
		cmd := c.command
		maxCmd := width - 20
		if maxCmd > 0 && len(cmd) > maxCmd {
			cmd = cmd[:maxCmd-3] + "..."
		}
		sb.WriteString(fmt.Sprintf("    %s %s  %s\n",
			lipgloss.NewStyle().Foreground(colorAmber).Render("$"),
			cmd,
			exitStyle.Render(tui.FormatExitCode(c.exitCode)),
		))
	}

	sb.WriteString("\n")

	flagLine := fmt.Sprintf("  %s  %d sensitive  %d blocked  %d errors",
		summaryLabelStyle.Render("Flags"),
		s.sensitive, s.blocked, s.errors,
	)
	if s.sensitive == 0 && s.blocked == 0 && s.errors == 0 {
		flagLine = dimStyle.Render(flagLine)
	} else if s.errors > 0 {
		flagLine = lipgloss.NewStyle().Foreground(colorRed).Render(flagLine)
	} else {
		flagLine = lipgloss.NewStyle().Foreground(colorAmber).Render(flagLine)
	}
	sb.WriteString(flagLine + "\n")

	return sb.String()
}
