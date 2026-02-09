package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
)

const schemaOutputPath = "schema/event.schema.json"

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
	Type        string   `json:"type,omitempty"`
	Description string   `json:"description,omitempty"`
	Format      string   `json:"format,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Ref         string   `json:"$ref,omitempty"`
	Const       string   `json:"const,omitempty"`
	OneOf       []oneOf  `json:"oneOf,omitempty"`
	Items       *items   `json:"items,omitempty"`
}

type oneOf struct {
	Type string `json:"type"`
}

type items struct {
	Type string `json:"type"`
}

type definition struct {
	Type        string              `json:"type"`
	Description string              `json:"description,omitempty"`
	Properties  map[string]property `json:"properties"`
	Required    []string            `json:"required,omitempty"`
}

func main() {
	schema := generateEventSchema()

	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal schema: %v\n", err)
		os.Exit(1)
	}

	data = append(data, '\n')

	if err := os.WriteFile(schemaOutputPath, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write schema file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Schema written to %s\n", schemaOutputPath)
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
