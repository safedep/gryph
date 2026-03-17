package query

import (
	"time"

	"github.com/safedep/gryph/storage"
)

type Options struct {
	Store       storage.Store
	Searcher    storage.Searcher
	Agents      []string
	Actions     []string
	Statuses    []string
	Since       time.Time
	Until       time.Time
	Session     string
	FilePattern string
	CmdPattern  string
	Sensitive   bool
}
