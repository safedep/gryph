package cli

import (
	"context"
	"testing"

	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCheck is a minimal security.Check implementation used to exercise
// the check factory registration path in NewApp.
type testCheck struct {
	name     string
	decision security.Decision
	reason   string
	guidance string
}

func (c *testCheck) Name() string  { return c.name }
func (c *testCheck) Enabled() bool { return true }
func (c *testCheck) Check(ctx context.Context, _ *events.Event) (*security.CheckResult, error) {
	return &security.CheckResult{
		Decision:  c.decision,
		Reason:    c.reason,
		Guidance:  c.guidance,
		CheckName: c.name,
	}, nil
}

// withCleanFactories isolates a test from other tests' RegisterCheckFactory
// calls by saving and restoring the package-level checkFactories slice.
func withCleanFactories(t *testing.T) {
	t.Helper()
	prev := checkFactories
	t.Cleanup(func() { checkFactories = prev })
	checkFactories = nil
}

func TestRegisterCheckFactory_RegisteredCheckIsInvoked(t *testing.T) {
	withCleanFactories(t)

	RegisterCheckFactory(func(cfg *config.Config) security.Check {
		return &testCheck{
			name:     "extension-block",
			decision: security.DecisionBlock,
			reason:   "factory-registered block for test",
		}
	})

	app, err := NewApp(config.Default())
	require.NoError(t, err)
	require.NotNil(t, app.Security)

	result := app.Security.Evaluate(context.Background(), &events.Event{})
	assert.False(t, result.IsAllowed(), "registered factory's blocking check should block evaluation")
	assert.Equal(t, "factory-registered block for test", result.BlockReason)
	assert.Equal(t, "extension-block", result.BlockedBy)
}

func TestRegisterCheckFactory_NilFactoryReturnIsIgnored(t *testing.T) {
	withCleanFactories(t)

	RegisterCheckFactory(func(cfg *config.Config) security.Check {
		return nil
	})

	app, err := NewApp(config.Default())
	require.NoError(t, err)

	// Only the built-in PlaceholderCheck should be active; it always allows.
	result := app.Security.Evaluate(context.Background(), &events.Event{})
	assert.True(t, result.IsAllowed(), "nil factory return should not register any check")
}

func TestRegisterCheckFactory_NilFunctionIsIgnored(t *testing.T) {
	withCleanFactories(t)

	// Registering a nil function is a caller bug, but we must not crash
	// or pollute the registry.
	RegisterCheckFactory(nil)

	app, err := NewApp(config.Default())
	require.NoError(t, err)

	result := app.Security.Evaluate(context.Background(), &events.Event{})
	assert.True(t, result.IsAllowed())
}

func TestNewApp_NoFactoriesStillBuildsCleanly(t *testing.T) {
	withCleanFactories(t)

	// Backward compatibility: if nobody registers a factory, NewApp behaves
	// exactly as it did before the extension point existed.
	app, err := NewApp(config.Default())
	require.NoError(t, err)
	require.NotNil(t, app.Security)

	result := app.Security.Evaluate(context.Background(), &events.Event{})
	assert.True(t, result.IsAllowed())
}

func TestRegisterCheckFactory_MultipleFactoriesAllInvoked(t *testing.T) {
	withCleanFactories(t)

	// Two factories, first allows, second blocks. Evaluator short-circuits
	// on the first block, so we expect blocked-by to be the second factory.
	RegisterCheckFactory(func(cfg *config.Config) security.Check {
		return &testCheck{name: "factory-a-allow", decision: security.DecisionAllow}
	})
	RegisterCheckFactory(func(cfg *config.Config) security.Check {
		return &testCheck{
			name:     "factory-b-block",
			decision: security.DecisionBlock,
			reason:   "second factory blocks",
		}
	})

	app, err := NewApp(config.Default())
	require.NoError(t, err)

	result := app.Security.Evaluate(context.Background(), &events.Event{})
	assert.False(t, result.IsAllowed())
	assert.Equal(t, "factory-b-block", result.BlockedBy)
}
