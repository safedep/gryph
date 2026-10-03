package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupervisorConfig_PilotUntil(t *testing.T) {
	future := time.Now().Add(72 * time.Hour).Format(time.RFC3339)
	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	tests := []struct {
		name      string
		cfg       SupervisorConfig
		profile   string
		remaining bool
	}{
		{"unset is enforce", SupervisorConfig{}, SupervisorProfileEnforce, false},
		{"enforce", SupervisorConfig{Profile: SupervisorProfileEnforce}, SupervisorProfileEnforce, false},
		{"pilot with a future end", SupervisorConfig{Profile: SupervisorProfilePilot, PilotUntil: future}, SupervisorProfilePilot, true},
		{"pilot past its end runs enforce", SupervisorConfig{Profile: SupervisorProfilePilot, PilotUntil: past}, SupervisorProfileEnforce, false},
		{"pilot with a date", SupervisorConfig{Profile: SupervisorProfilePilot, PilotUntil: time.Now().AddDate(0, 0, 3).Format("2006-01-02")}, SupervisorProfilePilot, true},
		{"pilot with no end is pilot until validation refuses it", SupervisorConfig{Profile: SupervisorProfilePilot}, SupervisorProfilePilot, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.profile, tt.cfg.EffectiveProfile())
			left, ok := tt.cfg.PilotRemaining()
			assert.Equal(t, tt.remaining, ok)
			if ok {
				assert.Positive(t, left)
			}
		})
	}
}

func TestValidate_PilotNeedsAnEnd(t *testing.T) {
	cfg := Default()
	cfg.Supervisor.Profile = SupervisorProfilePilot
	err := validate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pilot_until")

	cfg.Supervisor.PilotUntil = "next week"
	err = validate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a date")

	cfg.Supervisor.PilotUntil = "2026-12-31"
	assert.NoError(t, validate(cfg))
	cfg.Supervisor.PilotUntil = "2026-12-31T10:00:00Z"
	assert.NoError(t, validate(cfg))
	cfg.Supervisor.Profile = SupervisorProfileEnforce
	cfg.Supervisor.PilotUntil = ""
	assert.NoError(t, validate(cfg))
}

func TestParsePilotUntil_DateEndsThatDay(t *testing.T) {
	got, err := ParsePilotUntil("2026-03-01")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 3, 2, 0, 0, 0, 0, time.Local), got)
}
