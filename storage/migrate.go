package storage

import (
	"regexp"

	atlas "ariga.io/atlas/sql/schema"
	entschema "entgo.io/ent/dialect/sql/schema"
)

// migrateOptions are the options of every schema migration.
var migrateOptions = []entschema.MigrateOption{entschema.WithDiffHook(addColumnsInPlace)}

// constantDefault matches a column default that is a constant: a number,
// true, false, or a quoted string.
var constantDefault = regexp.MustCompile(`^(-?[0-9]+(\.[0-9]+)?|true|false|'([^']|'')*')$`)

// addColumnsInPlace lets SQLite add a new column with ALTER TABLE.
//
// Atlas copies the whole table to add a column when the column has an
// expression default or belongs to an index. Ent gives Atlas every default
// as an expression, also a constant such as false or 0. The first hook after
// an upgrade runs the migration. On a large audit database the copy holds
// the write lock for many seconds, concurrent hooks fail and lose their
// events, and an agent that kills the hook on its timeout makes the next
// hook start the copy again.
//
// A constant default becomes a literal, which ADD COLUMN accepts. A new
// index on a new column is a separate change of the same table, so Atlas
// can add the column first and create the index after it.
func addColumnsInPlace(next entschema.Differ) entschema.Differ {
	return entschema.DiffFunc(func(current, desired *atlas.Schema) ([]atlas.Change, error) {
		changes, err := next.Diff(current, desired)
		if err != nil {
			return nil, err
		}
		for _, change := range changes {
			if modify, ok := change.(*atlas.ModifyTable); ok {
				addInPlace(modify)
			}
		}
		return changes, nil
	})
}

func addInPlace(modify *atlas.ModifyTable) {
	added := make(map[*atlas.Index]bool)
	for _, c := range modify.Changes {
		if add, ok := c.(*atlas.AddIndex); ok {
			added[add.I] = true
		}
	}
	for _, c := range modify.Changes {
		add, ok := c.(*atlas.AddColumn)
		if !ok {
			continue
		}
		if raw, ok := add.C.Default.(*atlas.RawExpr); ok && constantDefault.MatchString(raw.X) {
			add.C.Default = &atlas.Literal{V: raw.X}
		}
		// SQLite refuses ADD COLUMN for a NOT NULL column with no constant
		// default. Such a column keeps its index, so Atlas copies the table.
		_, literal := add.C.Default.(*atlas.Literal)
		if (add.C.Type.Null || literal) && allAdded(add.C.Indexes, added) {
			add.C.Indexes = nil
		}
	}
}

func allAdded(indexes []*atlas.Index, added map[*atlas.Index]bool) bool {
	for _, idx := range indexes {
		if !added[idx] {
			return false
		}
	}
	return true
}
