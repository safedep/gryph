package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/safedep/gryph/internal/version"
	"github.com/spf13/cobra"
)

// NewAarmCmd is the root of the gryph aarm subcommand tree. Today it holds
// the conformance sub-command. Future AARM-specific tooling (e.g. policy
// drift detection vs the spec) lands here.
func NewAarmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aarm",
		Short: "AARM (Autonomous Action Runtime Management) tooling",
		Long: "AARM tooling. The 'conformance' subcommand runs the AARM " +
			"conformance suite and reports per-requirement pass/fail/skip " +
			"counts. The suite is independent of the AARM specification " +
			"version it tests via test/conformance/aarm/AARM_SPEC_VERSION.",
	}
	cmd.AddCommand(newAarmConformanceCmd())
	return cmd
}

type aarmConformanceFlags struct {
	format         string
	requirement    string
	includeShould  bool
	specOverride   string
	output         string
	suiteDirectory string
}

func newAarmConformanceCmd() *cobra.Command {
	flags := &aarmConformanceFlags{}
	cmd := &cobra.Command{
		Use:   "conformance",
		Short: "Run the AARM conformance suite",
		Long: "Runs the AARM conformance suite (or a prebuilt " +
			"gryph-conformance.test binary if shipped alongside gryph) and " +
			"emits a per-requirement pass/fail/skip report. --format json " +
			"output validates against test/conformance/aarm/report.schema.json.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAarmConformance(cmd, flags)
		},
	}
	cmd.Flags().StringVar(&flags.format, "format", "text", "report format: text | json | markdown")
	cmd.Flags().StringVar(&flags.requirement, "requirement", "all", "filter to one requirement (R1..R9) or 'all'")
	cmd.Flags().BoolVar(&flags.includeShould, "include-should", false, "treat SHOULD failures as exit-code-1")
	cmd.Flags().StringVar(&flags.specOverride, "spec-version", "", "override AARM_SPEC_VERSION on the report (default reads suite file)")
	cmd.Flags().StringVarP(&flags.output, "output", "o", "", "write report to FILE (default stdout)")
	cmd.Flags().StringVar(&flags.suiteDirectory, "suite-dir", "", "explicit path to the conformance suite directory (default: ./test/conformance/aarm)")
	return cmd
}

// runAarmConformance is the entry point for `gryph aarm conformance`. It
// resolves the test runner, drives it, parses the resulting test stream,
// renders the report in the chosen format, and exits per spec section 3.
func runAarmConformance(cmd *cobra.Command, flags *aarmConformanceFlags) error {
	started := time.Now().UTC()
	suiteDir, err := resolveSuiteDirectory(flags.suiteDirectory)
	if err != nil {
		return cliErr(2, err)
	}

	specVersion := flags.specOverride
	if specVersion == "" {
		specVersion = strings.TrimSpace(readSuiteFile(suiteDir, "AARM_SPEC_VERSION"))
	}
	suiteVersion := strings.TrimSpace(readSuiteFile(suiteDir, "SUITE_VERSION"))
	if suiteVersion == "" {
		suiteVersion = "0.0.0"
	}

	rawJSON, runErr := runConformanceBinary(suiteDir)
	if runErr != nil {
		return cliErr(2, fmt.Errorf("run conformance binary: %w", runErr))
	}

	report, err := buildReport(rawJSON, reportMetadata{
		AARMSpecVersion: specVersion,
		SuiteVersion:    suiteVersion,
		GryphCommit:     commitSuffix(),
		RanAt:           started.Truncate(time.Second),
		DurationMS:      time.Since(started).Milliseconds(),
	})
	if err != nil {
		return cliErr(2, fmt.Errorf("parse test events: %w", err))
	}
	if flags.requirement != "" && flags.requirement != "all" {
		report = filterReport(report, flags.requirement)
	}

	var writer = cmd.OutOrStdout()
	if flags.output != "" {
		f, err := os.Create(flags.output)
		if err != nil {
			return cliErr(2, fmt.Errorf("open output file: %w", err))
		}
		defer func() { _ = f.Close() }()
		writer = f
	}

	switch strings.ToLower(flags.format) {
	case "", "text":
		if err := renderText(writer, report); err != nil {
			return cliErr(2, err)
		}
	case "json":
		if err := renderJSON(writer, report); err != nil {
			return cliErr(2, err)
		}
	case "markdown", "md":
		if err := renderMarkdown(writer, report); err != nil {
			return cliErr(2, err)
		}
	default:
		return cliErr(2, fmt.Errorf("unknown --format %q (expected text|json|markdown)", flags.format))
	}

	if report.Summary.MUST.Failed > 0 || report.Summary.MUST.Errored > 0 {
		return cliErr(1, fmt.Errorf("MUST failures: %d failed, %d errored", report.Summary.MUST.Failed, report.Summary.MUST.Errored))
	}
	if flags.includeShould {
		if report.Summary.SHOULD.Failed > 0 || report.Summary.SHOULD.Errored > 0 {
			return cliErr(1, fmt.Errorf("SHOULD failures (--include-should): %d failed, %d errored", report.Summary.SHOULD.Failed, report.Summary.SHOULD.Errored))
		}
	}
	return nil
}

// cliErr wraps an error in the ExitCoder shape main.go consumes.
func cliErr(code int, err error) error {
	return WrapError(code, err.Error(), nil)
}

// resolveSuiteDirectory finds the conformance suite directory. Search order:
// explicit --suite-dir, ./test/conformance/aarm relative to cwd, then walking
// up to four parent directories looking for the same relative path.
func resolveSuiteDirectory(explicit string) (string, error) {
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return "", err
		}
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs, nil
		}
		return "", fmt.Errorf("--suite-dir %s does not exist or is not a directory", explicit)
	}
	candidates := []string{
		"test/conformance/aarm",
		"./test/conformance/aarm",
	}
	wd, err := os.Getwd()
	if err == nil {
		dir := wd
		for i := 0; i < 5; i++ {
			candidates = append(candidates, filepath.Join(dir, "test", "conformance", "aarm"))
			dir = filepath.Dir(dir)
		}
	}
	for _, c := range candidates {
		abs, _ := filepath.Abs(c)
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs, nil
		}
	}
	return "", errors.New("could not locate test/conformance/aarm; pass --suite-dir")
}

func readSuiteFile(dir, name string) string {
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// runConformanceBinary executes the conformance suite. Resolution order
// follows the spec: prefer the bundled gryph-conformance.test binary when
// one ships alongside the running gryph binary (driven through
// `go tool test2json` since stock test binaries do not implement -test.json
// themselves), else shell out to `go test -json ./...`. Returns the captured
// JSON event stream regardless of test exit code (failed tests still
// produce valid json events).
//
// Note: the bundled-binary path requires the `go` toolchain on PATH so we
// can drive it through `go tool test2json`. Stock Go test binaries emit
// plain text via `-test.v`, which the parser cannot consume. Returning an
// error in that situation is preferable to silently emitting an empty
// report with a zero exit code.
func runConformanceBinary(suiteDir string) ([]byte, error) {
	goBin, goErr := exec.LookPath("go")
	if path := findBundledBinary(); path != "" {
		if goErr != nil {
			return nil, errors.New("gryph-conformance.test found but the 'go' toolchain is required to run it via 'go tool test2json' (install Go, or invoke 'go test ./test/conformance/aarm/...' directly)")
		}
		return execAndCaptureJSON(goBin, []string{
			"tool", "test2json", "-t",
			path,
			"-test.v", "-test.run=.", "-test.timeout=120s",
		}, suiteDir)
	}
	if goErr != nil {
		return nil, errors.New("neither gryph-conformance.test (alongside gryph) nor 'go' (on PATH) found; cannot run the suite")
	}
	pkgRel := "./..."
	return execAndCaptureJSON(goBin, []string{"test", "-count=1", "-json", "-timeout=120s", pkgRel}, suiteDir)
}

func findBundledBinary() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	candidates := []string{
		filepath.Join(dir, "gryph-conformance.test"),
		filepath.Join(dir, "bin", "gryph-conformance.test"),
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c
		}
	}
	return ""
}

// EnvSuiteRunning is set on the test process spawned by `gryph aarm
// conformance`. The suite-of-suite tests check it to avoid an infinite
// recursion in which their own invocation re-runs the suite.
const EnvSuiteRunning = "GRYPH_AARM_CONFORMANCE_RUNNING"

// execAndCaptureJSON runs the test binary with cwd set to suiteDir so the
// fixture loaders find their files. The combined stdout+stderr stream is
// returned because `go test -json` writes JSON to stdout and the test
// process may write incidental warnings to stderr (we ignore them).
func execAndCaptureJSON(cmd string, args []string, suiteDir string) ([]byte, error) {
	c := exec.Command(cmd, args...)
	c.Dir = suiteDir
	c.Env = append(os.Environ(), EnvSuiteRunning+"=1")
	out, err := c.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Test failures are expected to set non-zero exit codes; the
			// json stream is still valid and contains the failure events.
			return out, nil
		}
		return out, err
	}
	return out, nil
}

// --- Test event parsing -------------------------------------------------

// testEvent is the subset of `go test -json` event we care about.
type testEvent struct {
	Time    time.Time `json:"Time"`
	Action  string    `json:"Action"`
	Package string    `json:"Package,omitempty"`
	Test    string    `json:"Test,omitempty"`
	Output  string    `json:"Output,omitempty"`
	Elapsed float64   `json:"Elapsed,omitempty"`
}

var (
	requiresRe = regexp.MustCompile(`AARM-REQUIRES:\s+requirement=(R[1-9][0-9]?)\s+tier=(MUST|SHOULD)\s+bullet=("(?:[^"\\]|\\.)*")`)
	skipRe     = regexp.MustCompile(`AARM-SKIP:\s+category=(not_implemented|out_of_scope|deferred|requires_external)\s+reason=("(?:[^"\\]|\\.)*")`)
	gapRe      = regexp.MustCompile(`AARM-GAP:\s+reason=("(?:[^"\\]|\\.)*")`)
)

type rawTest struct {
	name          string
	requirement   string
	tier          string
	bullet        string
	skipCategory  string
	skipReason    string
	gaps          []string
	status        string
	durationMS    int64
	failureOutput []string
}

// parseEvents builds per-test rawTest records from a JSON-event stream.
func parseEvents(stream []byte) (map[string]*rawTest, []string, error) {
	tests := map[string]*rawTest{}
	parserWarnings := []string{}
	scanner := bufio.NewScanner(bytes.NewReader(stream))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev testEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			parserWarnings = append(parserWarnings, fmt.Sprintf("non-JSON line ignored: %s", string(line)))
			continue
		}
		if ev.Test == "" {
			continue
		}
		rt := tests[ev.Test]
		if rt == nil {
			rt = &rawTest{name: ev.Test}
			tests[ev.Test] = rt
		}
		switch ev.Action {
		case "output":
			out := ev.Output
			if m := requiresRe.FindStringSubmatch(out); m != nil {
				rt.requirement = m[1]
				rt.tier = m[2]
				if b, err := unquote(m[3]); err == nil {
					rt.bullet = b
				}
			}
			if m := skipRe.FindStringSubmatch(out); m != nil {
				rt.skipCategory = m[1]
				if r, err := unquote(m[2]); err == nil {
					rt.skipReason = r
				}
			}
			if m := gapRe.FindStringSubmatch(out); m != nil {
				if r, err := unquote(m[1]); err == nil {
					rt.gaps = append(rt.gaps, r)
				}
			}
			if rt.status == "fail" || ev.Output != "" {
				rt.failureOutput = append(rt.failureOutput, out)
			}
		case "pass":
			rt.status = "pass"
			rt.durationMS = int64(ev.Elapsed * 1000)
		case "fail":
			rt.status = "fail"
			rt.durationMS = int64(ev.Elapsed * 1000)
		case "skip":
			if rt.status == "" {
				rt.status = "skip"
			}
			rt.durationMS = int64(ev.Elapsed * 1000)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, parserWarnings, err
	}
	// Promote unattributed sub-tests by inheriting from their parent.
	for name, rt := range tests {
		if rt.requirement != "" {
			continue
		}
		if i := strings.Index(name, "/"); i > 0 {
			parent := tests[name[:i]]
			if parent != nil && parent.requirement != "" {
				rt.requirement = parent.requirement
				rt.tier = parent.tier
				rt.bullet = parent.bullet
			}
		}
	}
	return tests, parserWarnings, nil
}

func unquote(s string) (string, error) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return s, nil
	}
	var out strings.Builder
	i := 1
	for i < len(s)-1 {
		c := s[i]
		if c == '\\' && i+1 < len(s)-1 {
			next := s[i+1]
			switch next {
			case 'n':
				out.WriteByte('\n')
			case 't':
				out.WriteByte('\t')
			case 'r':
				out.WriteByte('\r')
			case '"':
				out.WriteByte('"')
			case '\\':
				out.WriteByte('\\')
			default:
				out.WriteByte(next)
			}
			i += 2
			continue
		}
		out.WriteByte(c)
		i++
	}
	return out.String(), nil
}

// --- Report building ----------------------------------------------------

type reportMetadata struct {
	AARMSpecVersion string
	SuiteVersion    string
	GryphCommit     string
	RanAt           time.Time
	DurationMS      int64
}

// Report is the in-memory representation of the conformance report. Field
// names use json tags that match report.schema.json exactly.
type Report struct {
	SchemaVersion   string                `json:"schema_version"`
	AARMSpecVersion string                `json:"aarm_spec_version"`
	SuiteVersion    string                `json:"suite_version"`
	GryphCommit     string                `json:"gryph_commit"`
	RanAt           string                `json:"ran_at"`
	DurationMS      int64                 `json:"duration_ms,omitempty"`
	Summary         Summary               `json:"summary"`
	Requirements    []Requirement         `json:"requirements"`
	Gaps            []Gap                 `json:"gaps,omitempty"`
	Unattributed    []UnattributedResult  `json:"unattributed,omitempty"`
}

type Summary struct {
	MUST   TierCounts `json:"must"`
	SHOULD TierCounts `json:"should"`
}

type TierCounts struct {
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	Errored int `json:"errored"`
}

type Requirement struct {
	ID    string       `json:"id"`
	Title string       `json:"title"`
	Tests []TestResult `json:"tests"`
}

type TestResult struct {
	Name          string `json:"name"`
	Tier          string `json:"tier"`
	Bullet        string `json:"bullet,omitempty"`
	Status        string `json:"status"`
	DurationMS    int64  `json:"duration_ms,omitempty"`
	SkipCategory  string `json:"skip_category,omitempty"`
	SkipReason    string `json:"skip_reason,omitempty"`
	FailureOutput string `json:"failure_output,omitempty"`
}

type Gap struct {
	Requirement string `json:"requirement"`
	Tier        string `json:"tier"`
	Name        string `json:"name"`
	Category    string `json:"category,omitempty"`
	Reason      string `json:"reason"`
}

type UnattributedResult struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

const reportSchemaVersion = "1"

func buildReport(stream []byte, meta reportMetadata) (*Report, error) {
	tests, _, err := parseEvents(stream)
	if err != nil {
		return nil, err
	}
	byReq := map[string][]TestResult{}
	var unattributed []UnattributedResult
	var gaps []Gap
	summary := Summary{}

	for _, rt := range tests {
		if rt.status == "" {
			continue
		}
		if rt.requirement == "" {
			unattributed = append(unattributed, UnattributedResult{
				Name:       rt.name,
				Status:     rt.status,
				DurationMS: rt.durationMS,
				Reason:     "missing AARM-REQUIRES sentinel",
			})
			continue
		}
		tr := TestResult{
			Name:       trimTestName(rt.name),
			Tier:       rt.tier,
			Bullet:     rt.bullet,
			Status:     rt.status,
			DurationMS: rt.durationMS,
		}
		if rt.status == "skip" {
			cat := rt.skipCategory
			if cat == "" {
				cat = "not_implemented"
			}
			tr.SkipCategory = cat
			tr.SkipReason = rt.skipReason
			tr.DurationMS = 0
		}
		if rt.status == "fail" && len(rt.failureOutput) > 0 {
			tr.FailureOutput = strings.TrimSpace(strings.Join(rt.failureOutput, ""))
		}
		byReq[rt.requirement] = append(byReq[rt.requirement], tr)
		bumpCounts(&summary, rt.tier, rt.status)

		if rt.status == "fail" {
			gaps = append(gaps, Gap{
				Requirement: rt.requirement,
				Tier:        rt.tier,
				Name:        trimTestName(rt.name),
				Category:    "fail",
				Reason:      first(rt.gaps, "test failed"),
			})
		} else if rt.status == "skip" {
			gaps = append(gaps, Gap{
				Requirement: rt.requirement,
				Tier:        rt.tier,
				Name:        trimTestName(rt.name),
				Category:    tr.SkipCategory,
				Reason:      rt.skipReason,
			})
		} else if len(rt.gaps) > 0 {
			for _, g := range rt.gaps {
				gaps = append(gaps, Gap{
					Requirement: rt.requirement,
					Tier:        rt.tier,
					Name:        trimTestName(rt.name),
					Reason:      g,
				})
			}
		}
	}

	requirements := make([]Requirement, 0, len(byReq))
	keys := make([]string, 0, len(byReq))
	for k := range byReq {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return requirementSortKey(keys[i]) < requirementSortKey(keys[j]) })
	for _, k := range keys {
		rs := byReq[k]
		sort.Slice(rs, func(i, j int) bool { return rs[i].Name < rs[j].Name })
		requirements = append(requirements, Requirement{
			ID:    k,
			Title: requirementTitle(k),
			Tests: rs,
		})
	}
	sort.Slice(gaps, func(i, j int) bool {
		if gaps[i].Requirement != gaps[j].Requirement {
			return requirementSortKey(gaps[i].Requirement) < requirementSortKey(gaps[j].Requirement)
		}
		if gaps[i].Tier != gaps[j].Tier {
			return gaps[i].Tier < gaps[j].Tier
		}
		return gaps[i].Name < gaps[j].Name
	})
	sort.Slice(unattributed, func(i, j int) bool { return unattributed[i].Name < unattributed[j].Name })

	return &Report{
		SchemaVersion:   reportSchemaVersion,
		AARMSpecVersion: meta.AARMSpecVersion,
		SuiteVersion:    meta.SuiteVersion,
		GryphCommit:     meta.GryphCommit,
		RanAt:           meta.RanAt.UTC().Format("2006-01-02T15:04:05Z"),
		DurationMS:      meta.DurationMS,
		Summary:         summary,
		Requirements:    requirements,
		Gaps:            gaps,
		Unattributed:    unattributed,
	}, nil
}

func first(xs []string, fallback string) string {
	if len(xs) == 0 {
		return fallback
	}
	return xs[0]
}

func bumpCounts(s *Summary, tier, status string) {
	var t *TierCounts
	switch tier {
	case "MUST":
		t = &s.MUST
	case "SHOULD":
		t = &s.SHOULD
	default:
		return
	}
	switch status {
	case "pass":
		t.Passed++
	case "fail":
		t.Failed++
	case "skip":
		t.Skipped++
	case "error":
		t.Errored++
	}
}

func requirementSortKey(id string) int {
	if len(id) < 2 || id[0] != 'R' {
		return 999
	}
	v := 0
	for _, c := range id[1:] {
		if c < '0' || c > '9' {
			return 999
		}
		v = v*10 + int(c-'0')
	}
	return v
}

func requirementTitle(id string) string {
	switch id {
	case "R1":
		return "Pre-Execution Mediation"
	case "R2":
		return "Context Awareness"
	case "R3":
		return "Policy Evaluation"
	case "R4":
		return "Decision Outcomes"
	case "R5":
		return "Tamper-Evident Receipts"
	case "R6":
		return "Identity and Attribution"
	case "R7":
		return "Semantic Distance"
	case "R8":
		return "Telemetry"
	case "R9":
		return "Least Privilege"
	}
	return id
}

func trimTestName(name string) string {
	if strings.HasPrefix(name, "Test") {
		return name[len("Test"):]
	}
	return name
}

func filterReport(r *Report, requirement string) *Report {
	target := strings.ToUpper(strings.TrimSpace(requirement))
	out := *r
	out.Requirements = nil
	for _, req := range r.Requirements {
		if req.ID == target {
			out.Requirements = append(out.Requirements, req)
		}
	}
	out.Gaps = nil
	for _, g := range r.Gaps {
		if g.Requirement == target {
			out.Gaps = append(out.Gaps, g)
		}
	}
	out.Summary = Summary{}
	for _, req := range out.Requirements {
		for _, tr := range req.Tests {
			bumpCounts(&out.Summary, tr.Tier, tr.Status)
		}
	}
	return &out
}

// --- Renderers ----------------------------------------------------------

func renderJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// errWriter buffers the first write error so renderText / renderMarkdown can
// scribble through Fprintf without checking each call site, then surface the
// error in one place at the end.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(format string, args ...any) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintf(e.w, format, args...)
}

func (e *errWriter) println(args ...any) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintln(e.w, args...)
}

func renderText(w io.Writer, r *Report) error {
	ew := &errWriter{w: w}
	ew.printf("AARM Conformance Report\n")
	ew.printf("=======================\n\n")
	ew.printf("AARM spec version : %s\n", r.AARMSpecVersion)
	ew.printf("Suite version     : %s\n", r.SuiteVersion)
	ew.printf("Gryph commit      : %s\n", r.GryphCommit)
	ew.printf("Ran at            : %s\n\n", r.RanAt)
	ew.printf("Summary\n")
	ew.printf("  MUST   : %d passed, %d failed, %d skipped, %d errored\n",
		r.Summary.MUST.Passed, r.Summary.MUST.Failed, r.Summary.MUST.Skipped, r.Summary.MUST.Errored)
	ew.printf("  SHOULD : %d passed, %d failed, %d skipped, %d errored\n\n",
		r.Summary.SHOULD.Passed, r.Summary.SHOULD.Failed, r.Summary.SHOULD.Skipped, r.Summary.SHOULD.Errored)

	for _, req := range r.Requirements {
		ew.printf("%s %s\n", req.ID, req.Title)
		ew.printf("%s\n", strings.Repeat("-", len(req.ID)+1+len(req.Title)))
		for _, tr := range req.Tests {
			marker := statusGlyph(tr.Status)
			line := fmt.Sprintf("  %s [%s] %s", marker, tr.Tier, tr.Name)
			if tr.Bullet != "" {
				line += " - " + tr.Bullet
			}
			ew.println(line)
			switch tr.Status {
			case "skip":
				ew.printf("      skip(%s): %s\n", tr.SkipCategory, tr.SkipReason)
			case "fail":
				if tr.FailureOutput != "" {
					for _, ln := range strings.Split(strings.TrimRight(tr.FailureOutput, "\n"), "\n") {
						ew.printf("      %s\n", ln)
					}
				}
			}
		}
		ew.println()
	}

	if len(r.Unattributed) > 0 {
		ew.printf("Unattributed (missing AARM-REQUIRES sentinel)\n")
		for _, u := range r.Unattributed {
			ew.printf("  - %s [%s]\n", u.Name, u.Status)
		}
		ew.println()
	}
	return ew.err
}

func renderMarkdown(w io.Writer, r *Report) error {
	ew := &errWriter{w: w}
	ew.printf("# AARM Conformance Report\n\n")
	ew.printf("- AARM spec version: `%s`\n", r.AARMSpecVersion)
	ew.printf("- Suite version: `%s`\n", r.SuiteVersion)
	ew.printf("- Gryph commit: `%s`\n", r.GryphCommit)
	ew.printf("- Ran at: `%s`\n\n", r.RanAt)
	ew.printf("## Summary\n\n")
	ew.printf("| Tier | Passed | Failed | Skipped | Errored |\n")
	ew.printf("|---|---:|---:|---:|---:|\n")
	ew.printf("| MUST | %d | %d | %d | %d |\n",
		r.Summary.MUST.Passed, r.Summary.MUST.Failed, r.Summary.MUST.Skipped, r.Summary.MUST.Errored)
	ew.printf("| SHOULD | %d | %d | %d | %d |\n\n",
		r.Summary.SHOULD.Passed, r.Summary.SHOULD.Failed, r.Summary.SHOULD.Skipped, r.Summary.SHOULD.Errored)

	for _, req := range r.Requirements {
		ew.printf("## %s %s\n\n", req.ID, req.Title)
		ew.printf("| Tier | Test | Status | Notes |\n")
		ew.printf("|---|---|---|---|\n")
		for _, tr := range req.Tests {
			notes := tr.Bullet
			switch tr.Status {
			case "skip":
				notes = fmt.Sprintf("skip(%s): %s", tr.SkipCategory, tr.SkipReason)
			case "fail":
				notes = strings.TrimSpace(strings.ReplaceAll(tr.FailureOutput, "\n", "; "))
				if notes == "" {
					notes = "failed"
				}
			}
			ew.printf("| %s | `%s` | %s | %s |\n", tr.Tier, tr.Name, tr.Status, mdEscape(notes))
		}
		ew.println()
	}
	if len(r.Gaps) > 0 {
		ew.printf("## Gaps\n\n")
		for _, g := range r.Gaps {
			ew.printf("- `%s` (%s %s, %s): %s\n", g.Name, g.Requirement, g.Tier, g.Category, g.Reason)
		}
	}
	return ew.err
}

func mdEscape(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

func statusGlyph(status string) string {
	switch status {
	case "pass":
		return "PASS"
	case "fail":
		return "FAIL"
	case "skip":
		return "SKIP"
	case "error":
		return "ERR "
	}
	return "    "
}

func commitSuffix() string {
	c := version.Commit
	if c == "" {
		c = "unknown"
	}
	if isWorkingTreeDirty() {
		c += "-dirty"
	}
	return c
}

func isWorkingTreeDirty() bool {
	c := exec.Command("git", "status", "--porcelain")
	out, err := c.Output()
	if err != nil {
		return false
	}
	return len(bytes.TrimSpace(out)) > 0
}
