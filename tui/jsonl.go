package tui

import (
	"encoding/json"
	"io"
)

// JSONLPresenter renders output as newline-delimited JSON.
type JSONLPresenter struct {
	w       io.Writer
	encoder *json.Encoder
}

// NewJSONLPresenter creates a new JSONL presenter.
func NewJSONLPresenter(opts PresenterOptions) *JSONLPresenter {
	encoder := json.NewEncoder(opts.Writer)
	// No indentation for JSONL
	return &JSONLPresenter{
		w:       opts.Writer,
		encoder: encoder,
	}
}

// RenderStatus renders the tool status as JSONL.
func (p *JSONLPresenter) RenderStatus(status *StatusView) error {
	return p.encoder.Encode(status)
}

// RenderSessions renders a list of sessions as JSONL (one per line).
func (p *JSONLPresenter) RenderSessions(sessions []*SessionView) error {
	for _, s := range sessions {
		if err := p.encoder.Encode(s); err != nil {
			return err
		}
	}
	return nil
}

// RenderSession renders a single session detail as JSONL.
func (p *JSONLPresenter) RenderSession(session *SessionView, events []*EventView) error {
	// First line: session
	if err := p.encoder.Encode(session); err != nil {
		return err
	}
	// Following lines: events
	for _, e := range events {
		if err := p.encoder.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

// RenderEvents renders a list of events as JSONL (one per line).
func (p *JSONLPresenter) RenderEvents(events []*EventView) error {
	for _, e := range events {
		if err := p.encoder.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

// RenderInstall renders the installation result as JSONL.
func (p *JSONLPresenter) RenderInstall(result *InstallView) error {
	return p.encoder.Encode(result)
}

// RenderUninstall renders the uninstallation result as JSONL.
func (p *JSONLPresenter) RenderUninstall(result *UninstallView) error {
	return p.encoder.Encode(result)
}

// RenderDoctor renders the doctor check results as JSONL.
func (p *JSONLPresenter) RenderDoctor(result *DoctorView) error {
	for _, check := range result.Checks {
		if err := p.encoder.Encode(check); err != nil {
			return err
		}
	}
	return nil
}

// RenderConfig renders the configuration as JSONL.
func (p *JSONLPresenter) RenderConfig(config *ConfigView) error {
	return p.encoder.Encode(config)
}

// RenderSelfAudits renders self-audit entries as JSONL (one per line).
func (p *JSONLPresenter) RenderSelfAudits(entries []*SelfAuditView) error {
	for _, e := range entries {
		if err := p.encoder.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

// RenderDiff renders a diff view as JSONL.
func (p *JSONLPresenter) RenderDiff(diff *DiffView) error {
	return p.encoder.Encode(diff)
}

// RenderError renders an error message as JSONL.
func (p *JSONLPresenter) RenderError(err error) error {
	output := struct {
		Error string `json:"error"`
	}{
		Error: err.Error(),
	}
	return p.encoder.Encode(output)
}

// RenderMessage renders a simple message as JSONL.
func (p *JSONLPresenter) RenderMessage(message string) error {
	output := struct {
		Message string `json:"message"`
	}{
		Message: message,
	}
	return p.encoder.Encode(output)
}

// RenderStreamSync renders stream sync results as JSONL (one per line).
func (p *JSONLPresenter) RenderStreamSync(result *StreamSyncView) error {
	for _, tr := range result.TargetResults {
		if err := p.encoder.Encode(tr); err != nil {
			return err
		}
	}
	return nil
}

// Ensure JSONLPresenter implements Presenter
var _ Presenter = (*JSONLPresenter)(nil)
