package events

import (
	"encoding/json"
	"slices"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/core/privacy"
)

// ForExport returns a copy of the event for a destination outside the
// machine, with the export profile applied to every content value. It
// decodes and encodes the payload again, so a row from before content labels
// has the same shape as a new row.
//
// The raw event holds every value without a label, so the copy keeps it only
// when the profile includes every value. A plain string field, such as
// error_message or a command description, has no label of its own. It gets
// the most restrictive treatment of any value in the event.
func (e *Event) ForExport(p privacy.ExportProfile) *Event {
	out := *e
	if !p.IncludesAll() {
		out.RawEvent = nil
	}
	out.DiffContent = e.legacyLabel(e.DiffContent, "")
	labels := []privacy.Label{out.DiffContent.Label}
	out.DiffContent, _ = p.Apply(out.DiffContent)

	payload, err := e.DecodePayload()
	if err != nil {
		log.Warnf("events: export %s payload: %v", e.ActionType, err)
	}
	if payload == nil && len(e.Payload) > 0 && !p.IncludesAll() {
		out.Payload = nil
	}
	privacy.Walk(payload, func(path string, t *privacy.Text) {
		*t = e.legacyLabel(*t, path)
		labels = append(labels, t.Label)
	})
	plain := plainTreatment(p, e.IsSensitive, labels...)
	out.ErrorMessage = treatPlain(plain, e.ErrorMessage)
	if payload == nil {
		return &out
	}

	privacy.Project(payload, p)
	projectPlainFields(payload, plain)
	keepHash := plain == privacy.TreatInclude && !e.IsSensitive &&
		!slices.ContainsFunc(labels, func(l privacy.Label) bool { return !l.DigestExportable() })
	projectContentHash(payload, p, keepHash)
	data, err := json.Marshal(payload)
	if err != nil {
		log.Warnf("events: export %s payload: %v", e.ActionType, err)
		out.Payload = nil
		return &out
	}
	out.Payload = data
	return &out
}

// legacyLabel fills a label that a row from before content labels lacks. A
// sensitive event marks every value secret, as the label step does. A
// prompt without an origin has the origin user.
func (e *Event) legacyLabel(t privacy.Text, path string) privacy.Text {
	if t.IsZero() {
		return t
	}
	if e.IsSensitive && !t.Label.HasClass(privacy.ClassSecret) {
		t.Label.Classes = append(slices.Clone(t.Label.Classes), privacy.ClassSecret)
	}
	if path == "prompt" && t.Label.Origin == "" {
		t.Label.Origin = privacy.OriginUser
	}
	return t
}

// plainTreatment returns the most restrictive treatment of the labels. An
// event without a label gets the treatment of a value that the agent wrote.
func plainTreatment(p privacy.ExportProfile, sensitive bool, labels ...privacy.Label) privacy.Treatment {
	base := privacy.Label{Origin: privacy.OriginAgent}
	if sensitive {
		base.Classes = []privacy.Class{privacy.ClassSecret}
	}
	treatment := p.Treatment(base)
	for _, l := range labels {
		if l.Origin == "" && len(l.Classes) == 0 {
			continue
		}
		treatment = privacy.Stricter(treatment, p.Treatment(l))
	}
	return treatment
}

func treatPlain(t privacy.Treatment, v string) string {
	switch {
	case v == "" || t == privacy.TreatInclude:
		return v
	case t == privacy.TreatRedact:
		return privacy.RedactedValue
	default:
		return ""
	}
}

// contentHashContext binds the keyed content hash, so that it does not
// match the keyed digest of a label.
const contentHashContext = "content_hash"

// projectContentHash keys the content hash of a file payload, or removes it
// when keep is false. The hash is a plain sha256 of all of the content, so it
// follows the digest rule of a label.
func projectContentHash(payload any, p privacy.ExportProfile, keep bool) {
	treat := func(hash string) string {
		if !keep {
			return ""
		}
		return p.KeyedDigest(contentHashContext, hash)
	}
	switch pl := payload.(type) {
	case *FileReadPayload:
		pl.ContentHash = treat(pl.ContentHash)
	case *FileWritePayload:
		pl.ContentHash = treat(pl.ContentHash)
	}
}

// projectPlainFields applies the treatment to the payload fields that hold
// content in a plain string.
func projectPlainFields(payload any, t privacy.Treatment) {
	if t == privacy.TreatInclude {
		return
	}
	switch p := payload.(type) {
	case *FileReadPayload:
		p.Pattern = treatPlain(t, p.Pattern)
	case *CommandExecPayload:
		p.Description = treatPlain(t, p.Description)
		for i := range p.Args {
			p.Args[i] = treatPlain(t, p.Args[i])
		}
	case *SessionEndPayload:
		p.Reason = treatPlain(t, p.Reason)
	case *NotificationPayload:
		p.Message = treatPlain(t, p.Message)
		p.Details = nil
	}
}
