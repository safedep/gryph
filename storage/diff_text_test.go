package storage

import (
	"testing"

	"github.com/safedep/gryph/core/privacy"
	"github.com/stretchr/testify/assert"
)

func TestDiffText(t *testing.T) {
	labeled := privacy.Label{Size: 3, Digest: privacy.Digest("+ab")}
	tests := []struct {
		name  string
		value string
		label privacy.Label
		want  privacy.Text
	}{
		{"row from before content labels", "+ab", privacy.Label{}, privacy.Text{Value: "+ab", Label: privacy.Label{Unclassified: true}}},
		{"labeled row", "+ab", labeled, privacy.Text{Value: "+ab", Label: labeled}},
		{"no diff", "", privacy.Label{}, privacy.Text{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, diffText(tt.value, tt.label))
		})
	}
}
