package pdp

import (
	"reflect"
	"strings"
	"text/template"
	"text/template/parse"

	"github.com/safedep/gryph/aarm/model"
)

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
	Kind       string
	Origin     string
	Source     string
	Hosts      []string
	ReadPaths  []string
	WritePaths []string
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
	TagsSeen            []string
	TagSeq              map[string]int64
	OriginsSeen         []string
	EntitiesSeen        []string
	EgressHosts         []string
	IntentAvailable     bool
	ActionsSinceIntent  int

	// SemanticDrift is a removed field. It stays at zero so that an older
	// message template still renders.
	SemanticDrift float64
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
		Kind:       string(action.Kind),
		Origin:     string(action.Origin),
		Source:     action.Source,
		Hosts:      action.Hosts(),
		ReadPaths:  action.ReadPaths(),
		WritePaths: action.WritePaths(),
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
		TagsSeen:            tagNames(snapshot.TagsSeen),
		TagSeq:              tagSeq(snapshot.TagsSeen),
		OriginsSeen:         nonNil(snapshot.OriginsSeen),
		EntitiesSeen:        snapshot.EntitiesSeen,
		EgressHosts:         snapshot.EgressHosts,
		IntentAvailable:     snapshot.IntentAvailable,
		ActionsSinceIntent:  snapshot.ActionsSinceIntent,
	}
}

// unknownTemplateField returns the first field path of the template that
// templateData does not have, such as ".Context.Drift". It walks every branch
// of if and else, so a field in a branch that empty data skips is found too.
// Inside range and with the dot changes, so it checks only their pipelines.
func unknownTemplateField(tmpl *template.Template) string {
	if tmpl.Tree == nil {
		return ""
	}
	var walk func(n parse.Node) string
	walkAll := func(nodes ...parse.Node) string {
		for _, n := range nodes {
			if n == nil || reflect.ValueOf(n).IsNil() {
				continue
			}
			if f := walk(n); f != "" {
				return f
			}
		}
		return ""
	}
	walk = func(n parse.Node) string {
		switch n := n.(type) {
		case *parse.ListNode:
			for _, c := range n.Nodes {
				if f := walk(c); f != "" {
					return f
				}
			}
		case *parse.ActionNode:
			return walkAll(n.Pipe)
		case *parse.PipeNode:
			for _, c := range n.Cmds {
				if f := walkAll(c); f != "" {
					return f
				}
			}
		case *parse.CommandNode:
			return walkAll(n.Args...)
		case *parse.IfNode:
			return walkAll(n.Pipe, n.List, n.ElseList)
		case *parse.RangeNode:
			return walkAll(n.Pipe)
		case *parse.WithNode:
			return walkAll(n.Pipe)
		case *parse.FieldNode:
			if !hasFieldPath(reflect.TypeOf(templateData{}), n.Ident) {
				return "." + strings.Join(n.Ident, ".")
			}
		}
		return ""
	}
	return walk(tmpl.Root)
}

// hasFieldPath reports whether the type has the chain of fields or methods.
// A map accepts any key.
func hasFieldPath(t reflect.Type, path []string) bool {
	for _, name := range path {
		if _, ok := t.MethodByName(name); ok {
			return true
		}
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		switch t.Kind() {
		case reflect.Map:
			return true
		case reflect.Struct:
			f, ok := t.FieldByName(name)
			if !ok {
				return false
			}
			t = f.Type
		default:
			return false
		}
	}
	return true
}
