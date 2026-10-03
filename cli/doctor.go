package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/internal/selfupdate"
	"github.com/safedep/gryph/internal/version"
	"github.com/safedep/gryph/selfprotect"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"
)

// NewDoctorCmd creates the doctor command.
func NewDoctorCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose issues with installation",
		Long: `Diagnose issues with installation.

Performs various health checks:
- Database file exists and is readable/writable
- Config file exists and is valid
- Agent hooks are installed and executable
- Database schema is up to date

It also prints the self-protection table: the level at which each Gryph
asset resists a change, and the profile that the levels earn.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			app, err := loadApp()
			if err != nil {
				return err
			}
			app.Presenter = tui.NewPresenter(getFormat(format), tui.PresenterOptions{
				Writer:    cmd.OutOrStdout(),
				UseColors: app.Config.ShouldUseColors(),
			})

			checker := selfupdate.NewChecker()
			updateCh := checker.CheckAsync(ctx, &selfupdate.CheckInput{Version: version.Version})

			view, err := tui.RunWithSpinner("Checking installation health...", func() (*tui.DoctorView, error) {
				v := &tui.DoctorView{
					AllOK: true,
				}

				// Check database file
				dbCheck := tui.DoctorCheck{
					Name:        "Database file",
					Description: "Check if database file exists and is accessible",
				}
				if _, err := os.Stat(app.Config.GetDatabasePath()); os.IsNotExist(err) {
					dbCheck.Status = tui.CheckFail
					dbCheck.Message = "Database file not found"
					dbCheck.Suggestion = "Run 'gryph install' to initialize"
					v.AllOK = false
				} else if err != nil {
					dbCheck.Status = tui.CheckFail
					dbCheck.Message = "Cannot access database file: " + err.Error()
					v.AllOK = false
				} else {
					dbCheck.Status = tui.CheckOK
					dbCheck.Message = app.Config.GetDatabasePath()
				}
				v.Checks = append(v.Checks, dbCheck)

				// Check config file
				configCheck := tui.DoctorCheck{
					Name:        "Config file",
					Description: "Check if config file exists and is valid",
				}
				if managed := config.ManagedConfigStatus(); managed.Exists && managed.Err != nil {
					configCheck.Status = tui.CheckFail
					configCheck.Message = managed.Path + " exists but Gryph ignores it: " + managed.Err.Error()
					configCheck.Suggestion = "Make the file and every directory above it owned by the system administrator, with no write access for other users"
					v.AllOK = false
				} else if managed.Exists {
					configCheck.Status = tui.CheckOK
					configCheck.Message = managed.Path + " (managed by the system)"
				} else if _, err := os.Stat(app.Paths.ConfigFile); os.IsNotExist(err) {
					configCheck.Status = tui.CheckWarn
					configCheck.Message = "Config file not found (using defaults)"
					configCheck.Suggestion = "Run 'gryph config set' to create"
				} else if err != nil {
					configCheck.Status = tui.CheckFail
					configCheck.Message = "Cannot access config file: " + err.Error()
					v.AllOK = false
				} else {
					configCheck.Status = tui.CheckOK
					configCheck.Message = app.Paths.ConfigFile
				}
				v.Checks = append(v.Checks, configCheck)

				// Check each agent's hooks
				for _, adapter := range app.Registry.All() {
					detection, _ := adapter.Detect(ctx)
					hookStatus, _ := adapter.Status(ctx)

					hookCheck := tui.DoctorCheck{
						Name:        adapter.DisplayName() + " hooks",
						Description: "Check if hooks are installed and valid",
					}

					if detection == nil || !detection.Installed {
						hookCheck.Status = tui.CheckWarn
						hookCheck.Message = "Agent not installed"
					} else if hookStatus == nil || !hookStatus.Installed {
						hookCheck.Status = tui.CheckWarn
						hookCheck.Message = "Hooks not installed"
						hookCheck.Suggestion = "Run 'gryph install --agent " + adapter.Name() + "'"
					} else if !hookStatus.Valid {
						hookCheck.Status = tui.CheckFail
						hookCheck.Message = "Hooks are invalid"
						if len(hookStatus.Issues) > 0 {
							hookCheck.Message += ": " + hookStatus.Issues[0]
						}
						hookCheck.Suggestion = "Run 'gryph install --force --agent " + adapter.Name() + "'"
						v.AllOK = false
					} else if missing := agent.MissingPromptHooks(adapter.Hooks(), hookStatus.Hooks); len(missing) > 0 {
						hookCheck.Status = tui.CheckWarn
						hookCheck.Message = "Prompt hook not installed: " + strings.Join(missing, ", ")
						hookCheck.Suggestion = "Run 'gryph install --force --agent " + adapter.Name() + "' to record prompts"
					} else if old := hooksAboveVersion(adapter.Hooks(), detection.Version); len(old) > 0 {
						hookCheck.Status = tui.CheckWarn
						hookCheck.Message = fmt.Sprintf("%s %s is older than these hooks need: %s", adapter.DisplayName(), detection.Version, strings.Join(old, ", "))
						hookCheck.Suggestion = "Upgrade " + adapter.DisplayName()
					} else {
						hookCheck.Status = tui.CheckOK
						hookCheck.Message = "All hooks installed and valid"
					}
					v.Checks = append(v.Checks, hookCheck)
				}

				// Check database schema
				schemaCheck := tui.DoctorCheck{
					Name:        "Database schema",
					Description: "Check if database schema is up to date",
				}
				if err := app.InitStore(ctx); err != nil {
					schemaCheck.Status = tui.CheckFail
					schemaCheck.Message = "Cannot connect to database: " + err.Error()
					schemaCheck.Suggestion = "Run 'gryph install' to reinitialize"
					v.AllOK = false
				} else {
					schemaCheck.Status = tui.CheckOK
					schemaCheck.Message = "Schema is up to date"
				}
				v.Checks = append(v.Checks, schemaCheck)

				statuses := app.ProtectionProvider().Assess(ctx)
				v.Profile = string(selfprotect.ProfileOf(statuses))
				v.TamperRecorded = recordTamper(ctx, app, statuses)
				for _, s := range statuses {
					v.Protection = append(v.Protection, tui.ProtectionRow{
						Asset:    string(s.Asset),
						Agent:    s.Agent,
						Level:    s.Level.String(),
						Provider: s.Provider,
						Drift:    s.Drift,
						Detail:   s.Detail,
					})
				}

				return v, nil
			})
			if err != nil {
				return err
			}

			defer func() {
				err := app.Close()
				if err != nil {
					log.Errorf("failed to close app: %w", err)
				}
			}()

			if err := app.Presenter.RenderDoctor(view); err != nil {
				return err
			}

			renderUpdateNotice(app.Presenter, updateCh)
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json, jsonl, csv")

	return cmd
}

// recordTamper writes a tamper event for each change since the last run
// and returns the count. A failure to record is a warning, because the
// table above still shows the state.
func recordTamper(ctx context.Context, app *App, statuses []selfprotect.AssetStatus) int {
	recorder, err := app.TamperRecorder()
	if err != nil {
		log.Warnf("doctor: tamper events not recorded: %v", err)
		return 0
	}
	recorded, err := recorder.RecordChanges(ctx, statuses)
	if err != nil {
		log.Warnf("doctor: tamper events not recorded: %v", err)
	}
	return len(recorded)
}

// hooksAboveVersion returns the hooks, with their minimum version, that the
// detected agent version is too old to fire. An unknown version gives none.
func hooksAboveVersion(hooks []events.HookSpec, version string) []string {
	current := canonicalVersion(version)
	if current == "" {
		return nil
	}
	var old []string
	for _, h := range hooks {
		if h.MinVersion != "" && semver.Compare(current, canonicalVersion(h.MinVersion)) < 0 {
			old = append(old, fmt.Sprintf("%s (needs %s)", h.Type, h.MinVersion))
		}
	}
	return old
}

// canonicalVersion returns v as a canonical semver string, or "" when v is
// not a version. It drops a prerelease, so a preview or a nightly of the
// minimum version counts as that version.
func canonicalVersion(v string) string {
	v, _, _ = strings.Cut(strings.TrimSpace(v), "-")
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return ""
	}
	return semver.Canonical(v)
}
