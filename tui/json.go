package tui

import (
	"encoding/json"
	"io"
)

// JSONPresenter renders output as JSON.
type JSONPresenter struct {
	w       io.Writer
	encoder *json.Encoder
}

// NewJSONPresenter creates a new JSON presenter.
func NewJSONPresenter(opts PresenterOptions) *JSONPresenter {
	encoder := json.NewEncoder(opts.Writer)
	encoder.SetIndent("", "  ")
	return &JSONPresenter{
		w:       opts.Writer,
		encoder: encoder,
	}
}

// RenderStatus renders the tool status as JSON.
func (p *JSONPresenter) RenderStatus(status *StatusView) error {
	return p.encoder.Encode(status)
}

// RenderSessions renders a list of sessions as JSON.
func (p *JSONPresenter) RenderSessions(sessions []*SessionView) error {
	return p.encoder.Encode(sessions)
}

// RenderSession renders a single session detail as JSON.
func (p *JSONPresenter) RenderSession(session *SessionView, events []*EventView) error {
	output := struct {
		Session *SessionView   `json:"session"`
		Events  []*EventView   `json:"events"`
	}{
		Session: session,
		Events:  events,
	}
	return p.encoder.Encode(output)
}

// RenderEvents renders a list of events as JSON.
func (p *JSONPresenter) RenderEvents(events []*EventView) error {
	return p.encoder.Encode(events)
}

// RenderInstall renders the installation result as JSON.
func (p *JSONPresenter) RenderInstall(result *InstallView) error {
	return p.encoder.Encode(result)
}

// RenderUninstall renders the uninstallation result as JSON.
func (p *JSONPresenter) RenderUninstall(result *UninstallView) error {
	return p.encoder.Encode(result)
}

// RenderDoctor renders the doctor check results as JSON.
func (p *JSONPresenter) RenderDoctor(result *DoctorView) error {
	return p.encoder.Encode(result)
}

// RenderConfig renders the configuration as JSON.
func (p *JSONPresenter) RenderConfig(config *ConfigView) error {
	return p.encoder.Encode(config)
}

// RenderSelfAudits renders self-audit entries as JSON.
func (p *JSONPresenter) RenderSelfAudits(entries []*SelfAuditView) error {
	return p.encoder.Encode(entries)
}

// RenderDiff renders a diff view as JSON.
func (p *JSONPresenter) RenderDiff(diff *DiffView) error {
	return p.encoder.Encode(diff)
}

// RenderError renders an error message as JSON.
func (p *JSONPresenter) RenderError(err error) error {
	output := struct {
		Error string `json:"error"`
	}{
		Error: err.Error(),
	}
	return p.encoder.Encode(output)
}

// RenderMessage renders a simple message as JSON.
func (p *JSONPresenter) RenderMessage(message string) error {
	output := struct {
		Message string `json:"message"`
	}{
		Message: message,
	}
	return p.encoder.Encode(output)
}

// Ensure JSONPresenter implements Presenter
var _ Presenter = (*JSONPresenter)(nil)
