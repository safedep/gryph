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
	"github.com/safedep/gryph/core/events"
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
	Type string `json:"type,omitempty"`
	Ref  string `json:"$ref,omitempty"`
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

		if !strings.Contains(opts, "omitempty") {
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
				Description: "Ordered list of policy rules. Each rule produces a Decision; when multiple match the most restrictive wins (block > escalate > guidance > warn > allow).",
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
				Description: "Decision returned when the rule matches. `escalate` is reserved for approval workflows and currently degrades to `block`.",
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
				Description: "Free-form labels propagated to receipts (e.g. \"compliance\", \"pii\").",
				Items:       &items{Type: "string"},
			},
			"message": {
				Type:        "string",
				Description: "Go text/template rendered when the rule matches. Available references: .Action (Type, Tool, Operation, Agent, WorkingDir, Project, Params/Parameters), .Context (TotalActions, FilesRead, FilesWritten, CommandsExecuted, NetworkRequests, Errors, ToolsUsed, SessionDurationMs, ClassificationsSeen, EntitiesSeen, SemanticDrift), .Rule (ID, Description, Action, Severity, Tags).",
			},
			"match": {
				Ref: "#/$defs/match",
			},
			"scope": {
				Ref: "#/$defs/scope",
			},
			"condition": {
				Type:        "string",
				Description: "CEL expression returning bool. Evaluated after `match` succeeds. Variables: action.{type,tool,operation,agent,working_dir,project,params.{path,command,args,url,size_bytes,lines_added,lines_removed,content}}, context.{total_actions,files_read,files_written,commands_executed,network_requests,errors,tools_used,session_duration_ms,classifications_seen,entities_seen,semantic_drift}. Sandboxed; 100ms timeout.",
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
				Description: "Doublestar glob patterns matched against the action's file path. Forward-slash normalized.",
				Items:       &items{Type: "string"},
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

func fieldToProperty(field reflect.StructField) property {
	prop := property{}

	switch field.Type {
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

		if !strings.Contains(opts, "omitempty") {
			def.Required = append(def.Required, name)
		}
	}

	return def
}
