// Package tui provides the presentation layer for terminal output.
package tui

import (
	"io"
	"os"
)

// Format represents the output format.
type Format string

const (
	// FormatTable is the default table format.
	FormatTable Format = "table"
	// FormatJSON is JSON format.
	FormatJSON Format = "json"
	// FormatJSONL is newline-delimited JSON format.
	FormatJSONL Format = "jsonl"
	// FormatCSV is CSV format.
	FormatCSV Format = "csv"
)

// Presenter defines the interface for output rendering.
type Presenter interface {
	// RenderStatus renders the tool status.
	RenderStatus(status *StatusView) error

	// RenderSessions renders a list of sessions.
	RenderSessions(sessions []*SessionView) error

	// RenderSession renders a single session detail.
	RenderSession(session *SessionView, events []*EventView) error

	// RenderEvents renders a list of events.
	RenderEvents(events []*EventView) error

	// RenderEventDetails renders full details of one or more events.
	RenderEventDetails(events []*EventDetailView) error

	// RenderInstall renders the installation result.
	RenderInstall(result *InstallView) error

	// RenderUninstall renders the uninstallation result.
	RenderUninstall(result *UninstallView) error

	// RenderDoctor renders the doctor check results.
	RenderDoctor(result *DoctorView) error

	// RenderConfig renders the configuration.
	RenderConfig(config *ConfigView) error

	// RenderSelfAudits renders self-audit entries.
	RenderSelfAudits(entries []*SelfAuditView) error

	// RenderDiff renders a diff view.
	RenderDiff(diff *DiffView) error

	// RenderError renders an error message.
	RenderError(err error) error

	// RenderMessage renders a simple message.
	RenderMessage(message string) error

	// RenderStreamSync renders stream sync results.
	RenderStreamSync(result *StreamSyncView) error

	// RenderUpdateNotice renders an update availability notice.
	RenderUpdateNotice(notice *UpdateNoticeView) error
}

// PresenterOptions configures presenter behavior.
type PresenterOptions struct {
	// Writer is the output destination.
	Writer io.Writer
	// UseColors indicates if colors should be used.
	UseColors bool
	// Verbose increases output verbosity.
	Verbose bool
	// TerminalWidth is the width of the terminal for table rendering.
	// If 0, the width will be auto-detected.
	TerminalWidth int
}

// NewPresenter creates a new presenter for the given format.
func NewPresenter(format Format, opts PresenterOptions) Presenter {
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}

	switch format {
	case FormatJSON:
		return NewJSONPresenter(opts)
	case FormatJSONL:
		return NewJSONLPresenter(opts)
	case FormatCSV:
		return NewCSVPresenter(opts)
	default:
		return NewTablePresenter(opts)
	}
}

// DefaultPresenter returns a presenter with default options.
func DefaultPresenter() Presenter {
	return NewPresenter(FormatTable, PresenterOptions{
		Writer:    os.Stdout,
		UseColors: true,
	})
}
