package pep

import (
	"testing"

	"github.com/safedep/gryph/aarm/model"
	coresecurity "github.com/safedep/gryph/core/security"
	"github.com/stretchr/testify/assert"
)

func TestApply_SplitsAgentAndStoredReason(t *testing.T) {
	cases := []struct {
		name       string
		result     *model.EvaluationResult
		wantReason string
		wantStored string
	}{
		{
			name:       "block keeps both messages apart",
			result:     &model.EvaluationResult{Decision: model.DecisionBlock, FullMessage: "refused s3cr3t", Message: "refused"},
			wantReason: "refused s3cr3t",
			wantStored: "refused",
		},
		{
			name:       "empty stored message takes the default and not the full message",
			result:     &model.EvaluationResult{Decision: model.DecisionBlock, FullMessage: "s3cr3t"},
			wantReason: "s3cr3t",
			wantStored: blockedByPolicy,
		},
		{
			name:       "result with no full message uses the message",
			result:     &model.EvaluationResult{Decision: model.DecisionBlock, Message: "blocked"},
			wantReason: "blocked",
			wantStored: "blocked",
		},
		{
			name:       "unrouted defer keeps both messages apart",
			result:     &model.EvaluationResult{Decision: model.DecisionDefer, FullMessage: "wait s3cr3t", Message: "wait"},
			wantReason: "wait s3cr3t",
			wantStored: "wait",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Apply(tc.result)
			assert.Equal(t, coresecurity.DecisionBlock, got.Decision)
			assert.Equal(t, tc.wantReason, got.Reason)
			assert.Equal(t, tc.wantStored, got.StoredReason)
		})
	}
}
