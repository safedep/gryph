package cli

import (
	"context"
	"os"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

// NewDoctorCmd creates the doctor command.
func NewDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose issues with installation",
		Long: `Diagnose issues with installation.

Performs various health checks:
- Database file exists and is readable/writable
- Config file exists and is valid
- Agent hooks are installed and executable
- Database schema is up to date`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			app, err := loadApp()
			if err != nil {
				return err
			}

			view, err := tui.RunWithSpinner("Checking installation health...", func() (*tui.DoctorView, error) {
				v := &tui.DoctorView{
					AllOK: true,
				}

				// Check database file
				dbCheck := tui.DoctorCheck{
					Name:        "Database file",
					Description: "Check if database file exists and is accessible",
				}
				if _, err := os.Stat(app.Paths.DatabaseFile); os.IsNotExist(err) {
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
					dbCheck.Message = app.Paths.DatabaseFile
				}
				v.Checks = append(v.Checks, dbCheck)

				// Check config file
				configCheck := tui.DoctorCheck{
					Name:        "Config file",
					Description: "Check if config file exists and is valid",
				}
				if _, err := os.Stat(app.Paths.ConfigFile); os.IsNotExist(err) {
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

			return app.Presenter.RenderDoctor(view)
		},
	}

	return cmd
}
