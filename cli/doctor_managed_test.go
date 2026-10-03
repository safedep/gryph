package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderManagedDoctor(t *testing.T) {
	report := &managedDoctorReport{
		SchemaVersion: managedDoctorSchemaVersion,
		Profile:       "locked",
		Summary:       managedLockedSummary,
		Issues:        []string{},
		Config:        managedFileReport{Path: "/etc/safedep/gryph/config.yml", Chain: managedChainOK},
		Policy:        managedPolicyReport{managedFileReport: managedFileReport{Path: "/etc/safedep/gryph/policy.yaml", Chain: managedChainOK}, Version: "1", SHA256: "ab"},
		Binary:        managedFileReport{Path: "/opt/safedep/gryph/bin/gryph", Chain: managedChainOK},
		Agents:        []managedAgentState{{Name: "claude-code", Class: "locked", Path: "/etc/claude-code/x", Level: "prevent_same_user", Match: true, Locked: true}},
		Key:           managedKeyReport{Scope: managedKeyScopeUser},
		Supervisor:    managedSupervisorReport{State: managedSupervisorAbsent},
		Collection:    managedCollectionReport{Level: managedCollectionNone},
	}

	var text bytes.Buffer
	require.NoError(t, renderManagedDoctor(&text, report, false))
	assert.Contains(t, text.String(), "Managed doctor: "+managedLockedSummary)
	assert.Contains(t, text.String(), managedKeySummary)
	assert.Contains(t, text.String(), "claude-code  locked      prevent_same_user  /etc/claude-code/x  (locked)")
	assert.NotContains(t, text.String(), "Issues:")

	var out bytes.Buffer
	require.NoError(t, renderManagedDoctor(&out, report, true))
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &decoded))
	assert.Equal(t, float64(1), decoded["schema_version"])
	assert.Equal(t, "locked", decoded["profile"])
	assert.Equal(t, "user", decoded["key"].(map[string]any)["scope"])
	assert.Equal(t, false, decoded["key"].(map[string]any)["protected"])
	assert.Equal(t, "absent", decoded["supervisor"].(map[string]any)["state"])
	assert.Equal(t, "none", decoded["collection"].(map[string]any)["level"])
	assert.Equal(t, []any{}, decoded["issues"], "issues is a list, never null")

	report.Profile = "none"
	report.Issues = []string{"no managed policy at /etc/safedep/gryph/policy.yaml"}
	text.Reset()
	require.NoError(t, renderManagedDoctor(&text, report, false))
	assert.Contains(t, text.String(), "Issues:\n  - no managed policy")
}
