package pdp

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPolicySchema_NoDriftFromGoTypes(t *testing.T) {
	const schemaPath = "../../schema/policy.schema.json"
	data, err := os.ReadFile(schemaPath)
	require.NoError(t, err, "run `make generate-schema` to produce %s", schemaPath)

	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Defs       map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	require.NoError(t, json.Unmarshal(data, &schema))

	cases := []struct {
		typ          reflect.Type
		schemaFields map[string]json.RawMessage
		label        string
	}{
		{reflect.TypeOf(Policy{}), schema.Properties, "Policy vs schema.properties"},
		{reflect.TypeOf(Rule{}), schema.Defs["rule"].Properties, "Rule vs $defs.rule.properties"},
		{reflect.TypeOf(Match{}), schema.Defs["match"].Properties, "Match vs $defs.match.properties"},
		{reflect.TypeOf(Scope{}), schema.Defs["scope"].Properties, "Scope vs $defs.scope.properties"},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			require.NotNil(t, tc.schemaFields, "schema branch for %s is missing - regenerate", tc.label)
			for i := 0; i < tc.typ.NumField(); i++ {
				name := yamlFieldName(tc.typ.Field(i))
				if name == "" {
					continue
				}
				_, ok := tc.schemaFields[name]
				require.Truef(t, ok, "%s: field %q present in Go struct but missing from schema - regenerate via `make generate-schema`", tc.label, name)
			}
		})
	}
}

func yamlFieldName(f reflect.StructField) string {
	tag := f.Tag.Get("yaml")
	if tag == "" || tag == "-" {
		return ""
	}
	name := strings.SplitN(tag, ",", 2)[0]
	if name == "" || name == "-" {
		return ""
	}
	return name
}
