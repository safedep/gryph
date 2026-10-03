package approval

import (
	"testing"

	"github.com/safedep/gryph/aarm/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssurance_Order(t *testing.T) {
	assert.True(t, AssuranceLocalAdmin.Meets(AssuranceSameUserTTY))
	assert.True(t, AssuranceLocalAdmin.Meets(AssuranceLocalAdmin))
	assert.False(t, AssuranceSameUserTTY.Meets(AssuranceLocalAdmin))
	assert.True(t, AssuranceOutOfBand.Meets(""), "no floor accepts every known assurance")
	assert.False(t, Assurance("bogus").Meets(""), "an unknown assurance meets nothing")
	_, err := ParseAssurance("bogus")
	require.Error(t, err)
	a, err := ParseAssurance(" local-auth ")
	require.NoError(t, err)
	assert.Equal(t, AssuranceLocalAuth, a)
	assert.True(t, ScopeWindow.Wider(ScopeOnce))
	assert.False(t, ScopeOnce.Wider(ScopeSession))
	_, err = ParseScope("all")
	require.Error(t, err)
}

func TestActionDigest_CoversTheDirectory(t *testing.T) {
	a := &model.Action{Type: model.ActionCommandExec, Tool: "Bash", Agent: "claude-code", WorkingDir: "/home/user/project/", Parameters: model.Parameters{Command: "make deploy", Args: []string{"-j4"}}}
	b := *a
	b.WorkingDir = "/home/user/project"
	assert.Equal(t, ActionDigest(a), ActionDigest(&b), "a clean path is the identity")
	c := *a
	c.WorkingDir = "/home/user/other"
	assert.NotEqual(t, ActionDigest(a), ActionDigest(&c))
	d := *a
	d.Parameters.Content = "payload"
	assert.Equal(t, ActionDigest(a), ActionDigest(&d), "the content is not the action")
	assert.Equal(t, "", ActionDigest(nil))
	assert.Equal(t, "command_exec: make deploy", Summary(a))
	assert.Equal(t, "file_write: /x", Summary(&model.Action{Type: model.ActionFileWrite, Parameters: model.Parameters{Path: "/x"}}))
}

func TestOutcome_Meta(t *testing.T) {
	o := &Outcome{Channel: "local-admin", Assurance: AssuranceLocalAdmin, Approver: "admin", PeerTrust: PeerTrustUnknown, Scope: ScopeOnce}
	m := o.Meta()
	assert.Equal(t, "local-admin", m["channel"])
	assert.Equal(t, "once", m["scope"])
	_, has := m["request_id"]
	assert.False(t, has)
	var none *Outcome
	assert.Nil(t, none.Meta())
}
