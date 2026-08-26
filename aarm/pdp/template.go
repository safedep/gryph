package pdp

import "github.com/safedep/gryph/aarm/model"

type templateData struct {
	Action  templateAction
	Context templateContext
	Rule    templateRule
}

type templateAction struct {
	Type       string
	Tool       string
	Operation  string
	Agent      string
	WorkingDir string
	Project    string
	Params     templateParams
}

type templateParams struct {
	Path         string
	Command      string
	Args         []string
	URL          string
	SizeBytes    int64
	LinesAdded   int
	LinesRemoved int
	Content      string
}

type templateContext struct {
	TotalActions        int
	FilesRead           int
	FilesWritten        int
	CommandsExecuted    int
	NetworkRequests     int
	Errors              int
	ToolsUsed           []string
	SessionDurationMs   int64
	ClassificationsSeen []string
	EntitiesSeen        []string
	SemanticDrift       float64
}

type templateRule struct {
	ID          string
	Description string
	Action      string
	Severity    string
	Tags        []string
}

func newTemplateAction(action *model.Action) templateAction {
	if action == nil {
		action = &model.Action{}
	}
	params := templateParams{
		Path:         action.Parameters.Path,
		Command:      action.Parameters.Command,
		Args:         action.Parameters.Args,
		URL:          action.Parameters.URL,
		SizeBytes:    action.Parameters.SizeBytes,
		LinesAdded:   action.Parameters.LinesAdded,
		LinesRemoved: action.Parameters.LinesRemoved,
		Content:      action.Parameters.Content,
	}
	return templateAction{
		Type:       string(action.Type),
		Tool:       action.Tool,
		Operation:  action.Operation,
		Agent:      action.Agent,
		WorkingDir: action.WorkingDir,
		Project:    action.Project,
		Params:     params,
	}
}

func newTemplateContext(snapshot *model.ContextSnapshot) templateContext {
	if snapshot == nil {
		snapshot = &model.ContextSnapshot{}
	}
	return templateContext{
		TotalActions:        snapshot.TotalActions,
		FilesRead:           snapshot.FilesRead,
		FilesWritten:        snapshot.FilesWritten,
		CommandsExecuted:    snapshot.CommandsExecuted,
		NetworkRequests:     snapshot.NetworkRequests,
		Errors:              snapshot.Errors,
		ToolsUsed:           snapshot.ToolsUsed,
		SessionDurationMs:   snapshot.SessionDuration.Milliseconds(),
		ClassificationsSeen: snapshot.ClassificationsSeen,
		EntitiesSeen:        snapshot.EntitiesSeen,
		SemanticDrift:       snapshot.SemanticDrift,
	}
}
