package supervisor

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const module = "github.com/safedep/gryph/"

// forbiddenImports are the packages that open a path the agent user names,
// or that look a home directory up. The supervisor runs above the agent
// user, so an import of one of them turns a client claim into a privileged
// file read. The hook side owns them.
var forbiddenImports = []string{
	module + "utils/projectdetection",
	module + "hookside",
	module + "agent/claudecode/transcript",
	module + "core/cost",
	module + "cli",
	"os/user",
}

// forbiddenDependencies must stay out of the whole dependency tree of the
// package, not only out of its own files. core/cost and os/user are not in
// this list: the wire format carries the cost totals as a claim, and the
// configuration and account packages resolve names through os/user.
var forbiddenDependencies = []string{
	module + "utils/projectdetection",
	module + "hookside",
	module + "agent/claudecode/transcript",
	module + "cli",
}

func TestSupervisorFilesImportNoClaimReader(t *testing.T) {
	fset := token.NewFileSet()
	var files []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// A test builds fixtures with the types of these packages and
		// never runs in the service. The tree check below covers what
		// the service links.
		if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, files)

	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		require.NoError(t, err, path)
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, forbidden := range forbiddenImports {
				assert.NotEqual(t, forbidden, p, "%s imports %s", path, p)
			}
		}
	}
}

func TestSupervisorDependencyTreeHasNoClaimReader(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", "./...").CombinedOutput()
	require.NoError(t, err, string(out))
	deps := strings.Fields(string(out))
	require.Contains(t, deps, module+"engine", "the listing covers the dependency tree")
	for _, forbidden := range forbiddenDependencies {
		assert.NotContains(t, deps, forbidden)
	}
}
