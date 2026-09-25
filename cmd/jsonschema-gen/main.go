package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/aarm/shellcmd"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
)

const (
	eventSchemaOutputPath  = "schema/event.schema.json"
	policySchemaOutputPath = "schema/policy.schema.json"
	policySchemaID         = "https://schemas.safedep.io/gryph/policy.schema.json"
)

type jsonSchema struct {
	Schema      string                `json:"$schema"`
	ID          string                `json:"$id"`
	Title       string                `json:"title"`
	Description string                `json:"description"`
	Type        string                `json:"type"`
	Properties  map[string]property   `json:"properties"`
	Required    []string              `json:"required"`
	Defs        map[string]definition `json:"$defs,omitempty"`
}

type property struct {
	Type        string              `json:"type,omitempty"`
	Description string              `json:"description,omitempty"`
	Format      string              `json:"format,omitempty"`
	Enum        []string            `json:"enum,omitempty"`
	Ref         string              `json:"$ref,omitempty"`
	Const       string              `json:"const,omitempty"`
	OneOf       []oneOf             `json:"oneOf,omitempty"`
	Items       *items              `json:"items,omitempty"`
	Properties  map[string]property `json:"properties,omitempty"`
	Required    []string            `json:"required,omitempty"`
	MinLength   int                 `json:"minLength,omitempty"`
}

type oneOf struct {
	Type string `json:"type"`
}

type items struct {
	Type    string   `json:"type,omitempty"`
	Ref     string   `json:"$ref,omitempty"`
	Enum    []string `json:"enum,omitempty"`
	Pattern string   `json:"pattern,omitempty"`
}

type definition struct {
	Type        string              `json:"type"`
	Description string              `json:"description,omitempty"`
	Properties  map[string]property `json:"properties"`
	Required    []string            `json:"required,omitempty"`
}

func main() {
	if err := writeSchema(eventSchemaOutputPath, generateEventSchema()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Schema written to %s\n", eventSchemaOutputPath)

	if err := writeSchema(policySchemaOutputPath, generatePolicySchema()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Schema written to %s\n", policySchemaOutputPath)
}

func writeSchema(path string, schema jsonSchema) error {
	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func generateEventSchema() jsonSchema {
	schema := jsonSchema{
		Schema:      "https://json-schema.org/draft/2020-12/schema",
		ID:          events.EventSchemaURL,
		Title:       "Gryph Event",
		Description: "An audit event recorded by gryph representing a single action performed by an AI coding agent.",
		Type:        "object",
		Properties:  make(map[string]property),
		Defs:        make(map[string]definition),
	}

	schema.Properties["$schema"] = property{
		Type:        "string",
		Description: "JSON Schema URL for validation.",
		Const:       events.EventSchemaURL,
	}

	eventType := reflect.TypeOf(events.Event{})
	schema.Required = []string{"$schema"}
	for i := range eventType.NumField() {
		field := eventType.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		name, opts := parseJSONTag(jsonTag)
		prop := fieldToProperty(field)

		schema.Properties[name] = prop

		if !isOptional(opts) {
			schema.Required = append(schema.Required, name)
		}
	}

	schema.Properties["action_type"] = property{
		Type:        "string",
		Description: "The category of action performed.",
		Enum:        actionTypeValues(),
	}

	schema.Properties["result_status"] = property{
		Type:        "string",
		Description: "The outcome of the action.",
		Enum:        resultStatusValues(),
	}

	addPayloadDefinitions(schema.Defs)

	return schema
}

func generatePolicySchema() jsonSchema {
	schema := jsonSchema{
		Schema:      "https://json-schema.org/draft/2020-12/schema",
		ID:          policySchemaID,
		Title:       "Gryph Security Policy",
		Description: "A Gryph security policy document. Authored as YAML or JSON; merged with other sources by the policy loader and evaluated by the AARM Policy Decision Point.",
		Type:        "object",
		Properties: map[string]property{
			"version": {
				Type:        "string",
				Description: "Schema version identifier. Reserved; \"1\" is the current value.",
			},
			"disabled": {
				Type:        "array",
				Description: "Rule IDs to suppress in the merged policy. Listed IDs are dropped regardless of which source defined the rule, allowing project-local policies to neutralize global rules without forking.",
				Items:       &items{Type: "string"},
			},
			"rules": {
				Type:        "array",
				Description: "Ordered list of policy rules. Each rule produces a Decision; when multiple match the most restrictive wins (block > escalate > defer > guidance > warn > allow).",
				Items:       &items{Ref: "#/$defs/rule"},
			},
		},
		Required: []string{"rules"},
		Defs:     map[string]definition{},
	}

	schema.Defs["rule"] = definition{
		Type:        "object",
		Description: "A single policy rule.",
		Required:    []string{"id", "action"},
		Properties: map[string]property{
			"id": {
				Type:        "string",
				Description: "Stable identifier for this rule. Must be unique across all merged sources.",
				MinLength:   1,
			},
			"description": {
				Type:        "string",
				Description: "Human-readable explanation of what the rule does and why.",
			},
			"action": {
				Type:        "string",
				Description: "Decision returned when the rule matches. `escalate` routes to the approval workflow. `defer` queues the action for operator resolution and blocks the agent in the meantime; requires a non-empty `reason`.",
				Enum:        decisionValues(),
			},
			"severity": {
				Type:        "string",
				Description: "Severity tag surfaced in audit and receipts. Does not affect evaluation order.",
				Enum:        severityValues(),
			},
			"enabled": {
				Type:        "boolean",
				Description: "When false, the rule is parsed and validated but skipped during evaluation. Default true.",
			},
			"tags": {
				Type:        "array",
				Description: "Labels for the event. Every matched rule adds its tags to the context entry, at any decision, and later rules read them in context.tags_seen. A rule with action allow and tags labels an event without changing the decision. gryph policy validate rejects a tag that does not match the pattern.",
				Items:       &items{Type: "string", Pattern: pdp.TagNamePattern},
			},
			"message": {
				Type:        "string",
				Description: "Go text/template rendered when the rule matches. Available references: .Action (Type, Tool, Operation, Agent, WorkingDir, Project, Kind, Origin, Source, Hosts, ReadPaths, WritePaths, Params), .Context (TotalActions, FilesRead, FilesWritten, CommandsExecuted, NetworkRequests, Errors, ToolsUsed, SessionDurationMs, ClassificationsSeen, TagsSeen, TagSeq, OriginsSeen, EntitiesSeen, EgressHosts, IntentAvailable, ActionsSinceIntent), .Rule (ID, Description, Action, Severity, Tags).",
			},
			"match": {
				Ref: "#/$defs/match",
			},
			"scope": {
				Ref: "#/$defs/scope",
			},
			"condition": {
				Type:        "string",
				Description: "CEL expression returning bool. Evaluated after `match` succeeds. Variables: action.{type,tool,operation,agent,working_dir,project,kind,origin,source,hosts,read_paths,write_paths,params.{path,command,args,url,size_bytes,lines_added,lines_removed,content}}, context.{total_actions,files_read,files_written,commands_executed,network_requests,errors,tools_used,session_duration_ms,classifications_seen,tags_seen,tag_seq,origins_seen,entities_seen,egress_hosts,entries,intent_available,actions_since_intent}. Function glob(path, pattern) matches a doublestar pattern. Sandboxed; 100ms timeout.",
			},
			"reason": {
				Type:        "string",
				Description: "Required when action is `defer`. Surfaces on the receipt's defer_reason and in the operator-facing block message.",
			},
		},
	}

	schema.Defs["match"] = definition{
		Type:        "object",
		Description: "Match criteria. An empty match block matches every action (subject to scope/condition).",
		Properties: map[string]property{
			"action_types": {
				Type:        "array",
				Description: "Action types this rule applies to.",
				Items:       &items{Type: "string"},
			},
			"file_patterns": {
				Type:        "array",
				Description: "Doublestar glob patterns matched against the action's file path. For command_exec actions, also matched against the shell targets that `file_access` selects. Forward-slash normalized.",
				Items:       &items{Type: "string"},
			},
			"file_access": {
				Type:        "array",
				Description: "The shell targets of a command_exec action that `file_patterns` match: `read`, `write`, or `remove`. The default is `[write, remove]`. Requires `file_patterns`.",
				Items:       &items{Type: "string", Enum: []string{string(shellcmd.AccessRead), string(shellcmd.AccessWrite), string(shellcmd.AccessRemove)}},
			},
			"command_patterns": {
				Type:        "array",
				Description: "Go regexp patterns matched against the executed command line.",
				Items:       &items{Type: "string"},
			},
			"tool_names": {
				Type:        "array",
				Description: "Tool names this rule applies to (case-insensitive equality).",
				Items:       &items{Type: "string"},
			},
			"content_patterns": {
				Type:        "array",
				Description: "Go regexp patterns matched against the captured content preview (write payload preview, command stdout preview, etc).",
				Items:       &items{Type: "string"},
			},
			"working_directory_patterns": {
				Type:        "array",
				Description: "Doublestar glob patterns matched against the action's working directory.",
				Items:       &items{Type: "string"},
			},
		},
	}

	schema.Defs["scope"] = definition{
		Type:        "object",
		Description: "Narrows the rule to selected agents, projects, or tools. Empty fields mean \"any\". AND-combined with `match`.",
		Properties: map[string]property{
			"agents": {
				Type:        "array",
				Description: "Agent identifiers (e.g. claude-code, cursor, gemini).",
				Items:       &items{Type: "string"},
			},
			"projects": {
				Type:        "array",
				Description: "Detected project names.",
				Items:       &items{Type: "string"},
			},
			"tools": {
				Type:        "array",
				Description: "Tool names (e.g. Bash, Write, WebFetch).",
				Items:       &items{Type: "string"},
			},
		},
	}

	return schema
}

func parseJSONTag(tag string) (string, string) {
	parts := strings.SplitN(tag, ",", 2)
	name := parts[0]
	opts := ""
	if len(parts) > 1 {
		opts = parts[1]
	}
	return name, opts
}

// isOptional reports whether a field with these JSON tag options can be
// absent from the output.
func isOptional(opts string) bool {
	return strings.Contains(opts, "omitempty") || strings.Contains(opts, "omitzero")
}

func fieldToProperty(field reflect.StructField) property {
	prop := property{}

	switch field.Type {
	case reflect.TypeOf(privacy.Text{}):
		prop.Ref = "#/$defs/text"
	case reflect.TypeOf(privacy.Label{}):
		prop.Ref = "#/$defs/label"
	case reflect.TypeOf(uuid.UUID{}):
		prop.Type = "string"
		prop.Format = "uuid"
	case reflect.TypeOf(time.Time{}):
		prop.Type = "string"
		prop.Format = "date-time"
	case reflect.TypeOf(json.RawMessage{}):
		prop.OneOf = []oneOf{{Type: "object"}, {Type: "array"}, {Type: "null"}}
	default:
		switch field.Type.Kind() {
		case reflect.String:
			prop.Type = "string"
		case reflect.Int, reflect.Int64:
			prop.Type = "integer"
		case reflect.Bool:
			prop.Type = "boolean"
		case reflect.Slice:
			prop.Type = "array"
			if field.Type.Elem().Kind() == reflect.String {
				prop.Items = &items{Type: "string"}
			}
		}
	}

	return prop
}

func actionTypeValues() []string {
	return []string{
		string(events.ActionFileRead),
		string(events.ActionFileWrite),
		string(events.ActionFileDelete),
		string(events.ActionCommandExec),
		string(events.ActionNetworkRequest),
		string(events.ActionToolUse),
		string(events.ActionSessionStart),
		string(events.ActionSessionEnd),
		string(events.ActionNotification),
		string(events.ActionSubagentStart),
		string(events.ActionSubagentStop),
		string(events.ActionUserPrompt),
		string(events.ActionUnknown),
	}
}

func resultStatusValues() []string {
	return []string{
		string(events.ResultSuccess),
		string(events.ResultError),
		string(events.ResultBlocked),
		string(events.ResultRejected),
	}
}

func decisionValues() []string {
	return []string{
		string(model.DecisionAllow),
		string(model.DecisionWarn),
		string(model.DecisionGuidance),
		string(model.DecisionBlock),
		string(model.DecisionEscalate),
		string(model.DecisionDefer),
	}
}

func severityValues() []string {
	out := make([]string, 0, len(model.AllSeverities))
	for _, s := range model.AllSeverities {
		out = append(out, string(s))
	}
	return out
}

func addPayloadDefinitions(defs map[string]definition) {
	defs["text"] = structToDefinition(
		reflect.TypeOf(privacy.Text{}),
		"Agent content with its privacy label. Export writes every row in this shape.",
	)
	label := structToDefinition(
		reflect.TypeOf(privacy.Label{}),
		"Privacy facts about one content value. Exporters read it to decide what leaves the machine.",
	)
	label.Properties["classes"] = property{Type: "array", Items: &items{Type: "string", Enum: privacy.Strings(privacy.AllClasses)}}
	label.Properties["origin"] = property{Type: "string", Enum: originValues()}
	defs["label"] = label

	defs["file_read_payload"] = structToDefinition(
		reflect.TypeOf(events.FileReadPayload{}),
		"Payload for file read actions.",
	)
	defs["file_write_payload"] = structToDefinition(
		reflect.TypeOf(events.FileWritePayload{}),
		"Payload for file write actions.",
	)
	defs["file_delete_payload"] = structToDefinition(
		reflect.TypeOf(events.FileDeletePayload{}),
		"Payload for file delete actions.",
	)
	defs["command_exec_payload"] = structToDefinition(
		reflect.TypeOf(events.CommandExecPayload{}),
		"Payload for command execution actions.",
	)
	defs["tool_use_payload"] = structToDefinition(
		reflect.TypeOf(events.ToolUsePayload{}),
		"Payload for tool use actions.",
	)
	defs["session_payload"] = structToDefinition(
		reflect.TypeOf(events.SessionPayload{}),
		"Payload for session start actions.",
	)
	defs["session_end_payload"] = structToDefinition(
		reflect.TypeOf(events.SessionEndPayload{}),
		"Payload for session end actions.",
	)
	defs["notification_payload"] = structToDefinition(
		reflect.TypeOf(events.NotificationPayload{}),
		"Payload for notification actions.",
	)
	defs["subagent_start_payload"] = structToDefinition(
		reflect.TypeOf(events.SubagentStartPayload{}),
		"Payload for subagent start actions.",
	)
	defs["subagent_stop_payload"] = structToDefinition(
		reflect.TypeOf(events.SubagentStopPayload{}),
		"Payload for subagent stop actions.",
	)
	defs["user_prompt_payload"] = structToDefinition(
		reflect.TypeOf(events.UserPromptPayload{}),
		"Payload for user prompt events.",
	)
}

func structToDefinition(t reflect.Type, desc string) definition {
	def := definition{
		Type:        "object",
		Description: desc,
		Properties:  make(map[string]property),
	}

	for i := range t.NumField() {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		name, opts := parseJSONTag(jsonTag)
		prop := fieldToProperty(field)
		def.Properties[name] = prop

		if !isOptional(opts) {
			def.Required = append(def.Required, name)
		}
	}

	return def
}

func originValues() []string {
	return []string{
		string(privacy.OriginUser), string(privacy.OriginAgent), string(privacy.OriginFileProject),
		string(privacy.OriginFileExternal), string(privacy.OriginCommand), string(privacy.OriginWeb),
		string(privacy.OriginMCP), string(privacy.OriginUnknown),
	}
}
