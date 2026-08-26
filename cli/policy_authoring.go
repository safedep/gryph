package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/loader"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

func newPolicyListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the active policy sources and rule counts",
		Long: "Lists every active policy source with its rule count: the global " +
			"file, each file in the policies directory, and the built-ins. A file " +
			"that fails to parse is shown with an error marker and does not hide " +
			"the rest. The final line reports the merged rule total, or a conflict " +
			"that stops the merge.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				log.Warnf("loadApp failed during policy list, using resolved defaults: %v", err)
			}
			cfg := appConfig(app)
			paths := appPaths(app)
			rows := gatherPolicyListRows(cfg, paths)
			merged, mergeErr := buildPolicyLoader(cfg, paths).Load(cmd.Context())
			renderPolicyList(cmd.OutOrStdout(), policyColorizer(app), rows, merged, mergeErr)
			return nil
		},
	}
}

type policyListRow struct {
	source string
	file   string
	rules  int
	err    string
}

// gatherPolicyListRows loads each source on its own so one broken file does not
// hide the rest. It shares source identity and file scanning with the loader.
func gatherPolicyListRows(cfg *config.Config, paths *config.Paths) []policyListRow {
	var rows []policyListRow

	globalPath := config.DefaultPolicyFilePath(paths)
	if fileExists(globalPath) {
		rows = append(rows, policyFileRow("global", globalPath))
	}

	dir := config.DefaultPolicyDirPath(paths)
	files, err := loader.PolicyFilesInDir(dir)
	if err != nil {
		rows = append(rows, policyListRow{source: "policies", file: dir, err: firstLine(err.Error())})
	}
	for _, f := range files {
		rows = append(rows, policyFileRow("policies", f))
	}

	if selfProtectionEnabled(cfg) {
		rows = append(rows, builtinListRow(cfg, paths))
	}
	return rows
}

func policyFileRow(source, path string) policyListRow {
	policy, err := pdp.LoadPolicyFile(path)
	if err != nil {
		return policyListRow{source: source, file: filepath.Base(path), err: firstLine(err.Error())}
	}
	return policyListRow{source: source, file: filepath.Base(path), rules: len(policy.Rules)}
}

func builtinListRow(cfg *config.Config, paths *config.Paths) policyListRow {
	src := loader.NewBuiltinSource(selfProtectionGlobs(cfg, paths)...)
	docs, err := src.Load(context.Background())
	if err != nil {
		return policyListRow{source: "builtin", file: "(embedded self-protection)", err: firstLine(err.Error())}
	}
	rules := 0
	for _, d := range docs {
		if d != nil {
			rules += len(d.Rules)
		}
	}
	return policyListRow{source: "builtin", file: "(embedded self-protection)", rules: rules}
}

func renderPolicyList(w io.Writer, c *tui.Colorizer, rows []policyListRow, merged *pdp.Policy, mergeErr error) {
	const srcHead, fileHead = "SOURCE", "FILE"
	srcWidth, fileWidth := len(srcHead), len(fileHead)
	for _, r := range rows {
		if len(r.source) > srcWidth {
			srcWidth = len(r.source)
		}
		if len(r.file) > fileWidth {
			fileWidth = len(r.file)
		}
	}

	_, _ = fmt.Fprintf(w, "%s  %s  %s\n",
		c.Dim(fmt.Sprintf("%-*s", srcWidth, srcHead)),
		c.Dim(fmt.Sprintf("%-*s", fileWidth, fileHead)),
		c.Dim("RULES"))
	for _, r := range rows {
		rulesCell := c.Number(fmt.Sprintf("%d", r.rules))
		if r.err != "" {
			rulesCell = c.Warning("! " + r.err)
		}
		_, _ = fmt.Fprintf(w, "%s  %s  %s\n",
			c.Cyan(fmt.Sprintf("%-*s", srcWidth, r.source)),
			fmt.Sprintf("%-*s", fileWidth, r.file),
			rulesCell)
	}

	_, _ = fmt.Fprintln(w)
	switch {
	case mergeErr != nil:
		_, _ = fmt.Fprintf(w, "%s %s\n", c.Warning("Merge conflict:"), firstLine(mergeErr.Error()))
	case merged != nil:
		_, _ = fmt.Fprintf(w, "%s\n", c.Success(fmt.Sprintf("%d rules merged.", len(merged.Rules))))
		if len(rows) == 1 && rows[0].source == "builtin" {
			_, _ = fmt.Fprintf(w, "%s\n", c.Dim("Only built-in rules are active. Run `gryph policy init` to author your own."))
		}
	}
}

func newPolicyInstallCmd() *cobra.Command {
	var (
		name   string
		force  bool
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:   "install <path>",
		Short: "Validate a candidate policy file and copy it into the policies directory",
		Long: "Validates a candidate policy file, then copies it into the policies " +
			"directory so it becomes active. The destination name is the source " +
			"basename, or <name>.yaml with --name. install refuses to overwrite an " +
			"existing file unless --force. --dry-run validates and shows the " +
			"destination without copying. install is the human-run promote step. An " +
			"agent cannot write into the protected policies directory with its file " +
			"tools.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				log.Warnf("loadApp failed during policy install, using resolved defaults: %v", err)
			}
			cfg := appConfig(app)
			paths := appPaths(app)

			src, err := expandUserPath(args[0])
			if err != nil {
				return err
			}
			candidate, err := pdp.LoadPolicyFile(src)
			if err != nil {
				return ErrConfig("candidate policy is not valid", err)
			}

			destName, err := installDestName(src, name)
			if err != nil {
				return err
			}
			dest := filepath.Join(config.DefaultPolicyDirPath(paths), destName)

			c := policyColorizer(app)
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "%s Validated %s (%d rules)\n", c.StatusOK(), c.Path(src), len(candidate.Rules))

			if err := checkMergedWithCandidate(cfg, paths, dest, src); err != nil {
				return err
			}

			if dryRun {
				_, _ = fmt.Fprintf(out, "  Would install to %s (dry run, nothing written)\n", c.Path(dest))
				if fileExists(dest) {
					_, _ = fmt.Fprintf(out, "  %s\n", c.Dim("Destination exists. install needs --force to overwrite."))
				}
				return nil
			}

			if fileExists(dest) && !force {
				return ErrConfig(fmt.Sprintf("refusing to overwrite %s (use --force to replace)", dest), fmt.Errorf("%s exists", dest))
			}
			if err := copyPolicyFile(src, dest); err != nil {
				return err
			}
			auditPolicyChange(cmd.Context(), app, "policy_install", dest)

			_, _ = fmt.Fprintf(out, "%s Installed to %s\n", c.StatusOK(), c.Path(dest))
			_, _ = fmt.Fprintf(out, "  %s\n", c.Dim("Run `gryph policy list` to confirm, `gryph policy test` to check behavior."))
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "destination file name (the .yaml suffix is optional)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing installed file")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate and show the destination without copying")
	return cmd
}

// installDestName resolves the destination file name for install. It rejects a
// name with a path component or a parent reference, so install always writes a
// direct YAML child of the policies directory that DirSource loads. It
// normalizes a missing extension to .yaml.
func installDestName(src, name string) (string, error) {
	raw := name
	if raw == "" {
		raw = filepath.Base(src)
	}
	if raw == "." || raw == ".." || strings.ContainsAny(raw, `/\`) {
		return "", ErrConfig("invalid policy name", fmt.Errorf("%q must be a plain file name without a path", raw))
	}
	return loader.NormalizePolicyFileName(raw), nil
}

// checkMergedWithCandidate loads the policy that would result after install, so
// a candidate that is valid alone but breaks the merge (a duplicate rule ID
// across files, a reserved prefix) is refused before it is copied. It excludes
// the file the candidate would replace, so a force re-install of an updated
// file does not collide with the version it replaces.
func checkMergedWithCandidate(cfg *config.Config, paths *config.Paths, dest, candidatePath string) error {
	if paths == nil {
		paths = config.ResolvePaths()
	}
	sources := []loader.Source{
		loader.NewOptionalFileSource(config.DefaultPolicyFilePath(paths)),
	}
	files, err := loader.PolicyFilesInDir(config.DefaultPolicyDirPath(paths))
	if err != nil {
		return ErrConfig("scan policies directory", err)
	}
	for _, f := range files {
		if filepath.Clean(f) == filepath.Clean(dest) {
			continue
		}
		sources = append(sources, loader.NewFileSource(f))
	}
	sources = append(sources, loader.NewFileSource(candidatePath))
	if selfProtectionEnabled(cfg) {
		sources = append(sources, loader.NewBuiltinSource(selfProtectionGlobs(cfg, paths)...))
	}
	if _, err := loader.New(sources...).Load(context.Background()); err != nil {
		return ErrConfig("candidate conflicts with the active policy", err)
	}
	return nil
}

func copyPolicyFile(src, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return ErrConfig("read candidate file", err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return ErrConfig("create policies directory", err)
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return ErrConfig("write installed policy", err)
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
