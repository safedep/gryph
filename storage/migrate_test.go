package storage

import (
	"testing"

	atlas "ariga.io/atlas/sql/schema"
	"github.com/stretchr/testify/assert"
)

func TestAddInPlace(t *testing.T) {
	column := func(name string, null bool, def atlas.Expr) *atlas.Column {
		return &atlas.Column{Name: name, Type: &atlas.ColumnType{Type: &atlas.IntegerType{T: "integer"}, Null: null}, Default: def}
	}
	tests := []struct {
		name        string
		column      *atlas.Column
		wantDefault atlas.Expr
		keepsIndex  bool
	}{
		{"constant default", column("imported", false, &atlas.RawExpr{X: "false"}), &atlas.Literal{V: "false"}, false},
		{"negative number", column("offset", false, &atlas.RawExpr{X: "-1"}), &atlas.Literal{V: "-1"}, false},
		{"quoted string", column("scope", false, &atlas.RawExpr{X: "'once'"}), &atlas.Literal{V: "'once'"}, false},
		{"expression", column("at", false, &atlas.RawExpr{X: "(CURRENT_TIMESTAMP)"}), &atlas.RawExpr{X: "(CURRENT_TIMESTAMP)"}, true},
		{"nullable with no default", column("tool_call_id", true, nil), nil, false},
		{"not null with no default", column("required", false, nil), nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index := &atlas.Index{Name: "idx"}
			tt.column.Indexes = []*atlas.Index{index}
			modify := &atlas.ModifyTable{Changes: []atlas.Change{
				&atlas.AddColumn{C: tt.column},
				&atlas.AddIndex{I: index},
			}}

			addInPlace(modify)

			assert.Equal(t, tt.wantDefault, tt.column.Default)
			assert.Equal(t, tt.keepsIndex, len(tt.column.Indexes) > 0)
		})
	}
}

// An index that the same change does not add stays on the column, so Atlas
// keeps its own decision for it.
func TestAddInPlace_IndexNotAdded(t *testing.T) {
	c := &atlas.Column{Name: "tool_call_id", Type: &atlas.ColumnType{Type: &atlas.StringType{T: "text"}, Null: true}}
	c.Indexes = []*atlas.Index{{Name: "existing"}}
	addInPlace(&atlas.ModifyTable{Changes: []atlas.Change{&atlas.AddColumn{C: c}}})
	assert.Len(t, c.Indexes, 1)
}
