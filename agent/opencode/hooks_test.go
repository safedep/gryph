package opencode

import (
	"testing"

	"github.com/safedep/gryph/agent/utils"
	"github.com/stretchr/testify/assert"
)

func TestProcessedPlugin_ReplacesPlaceholder(t *testing.T) {
	processed := processedPlugin()

	assert.NotContains(t, string(processed), utils.GryphCommandPlaceholder)
	assert.Contains(t, string(processed), utils.GryphCommand())
}
