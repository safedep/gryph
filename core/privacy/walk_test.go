package privacy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWalk(t *testing.T) {
	type inner struct {
		Note Text `json:"note"`
	}
	type outer struct {
		Command Text    `json:"command"`
		Plain   string  `json:"plain"`
		Inner   inner   `json:"inner"`
		Ptr     *inner  `json:"ptr"`
		List    []inner `json:"list"`
		hidden  Text
	}
	v := outer{
		Command: NewText("ls"),
		Ptr:     &inner{Note: NewText("p")},
		List:    []inner{{Note: NewText("a")}, {Note: NewText("b")}},
		hidden:  NewText("h"),
	}

	var paths []string
	Walk(&v, func(path string, t *Text) {
		paths = append(paths, path)
		t.Label.Level = "full"
	})

	assert.Equal(t, []string{"command", "inner.note", "ptr.note", "list.note", "list.note"}, paths)
	assert.Equal(t, "full", v.Command.Label.Level)
	assert.Equal(t, "full", v.Ptr.Note.Label.Level)
	assert.Equal(t, "full", v.List[1].Note.Label.Level)
	assert.Empty(t, v.hidden.Label.Level)

	assert.NotPanics(t, func() { Walk(v, func(string, *Text) {}) })
	assert.NotPanics(t, func() { Walk((*outer)(nil), func(string, *Text) {}) })
}
