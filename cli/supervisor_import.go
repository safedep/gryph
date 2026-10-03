package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/hookside"
	"github.com/safedep/gryph/internal/version"
	"github.com/safedep/gryph/storage"
	"github.com/spf13/cobra"
)

// importMarkerName is the file in the user's data directory that says the
// import ran to the end. A later run changes nothing until --force.
const importMarkerName = "import.done"

// importReport is the JSON contract of gryph supervisor import.
type importReport struct {
	Database string `json:"database"`
	// Sessions, Events and Receipts count the rows the service took.
	// Skipped counts the sessions the service already had.
	Sessions int `json:"sessions"`
	Events   int `json:"events"`
	Receipts int `json:"receipts"`
	Skipped  int `json:"skipped"`
	// Done is true when the import ran to the end and left its marker.
	Done bool   `json:"done"`
	Note string `json:"note,omitempty"`
}

func newSupervisorImportCmd() *cobra.Command {
	var (
		force  bool
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Carry this account's own database into the decision service",
		Long: `Carry this account's own database into the decision service.

Before a host gets the decision service, the hooks of an account record
into a database in the account's home. This command reads that database
as the account and sends its sessions, events and receipts over the
socket. The service stores them in the partition of this account, marked
imported, because the account could have changed them before the import.
The receipts keep their signatures and verify against the account's own
trust store. Root never opens the database: the command runs as the
account, and the reconcile job runs it once on a managed host.

A second run changes nothing: a session the service already has is
skipped, and the marker file in the data directory ends the run before
it starts. --force runs it again.`,
		Example: `  gryph supervisor import
  gryph supervisor import --force --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()
			app, err := loadApp()
			if err != nil {
				return err
			}
			if !clientMode(app.Config) {
				return NewCLIError(ExitGeneral, "supervisor import needs a managed configuration with the decision service on")
			}
			report, err := importUserDatabase(ctx, app, force)
			if err != nil {
				return WrapError(ExitGeneral, "import", err)
			}
			return renderImportReport(cmd.OutOrStdout(), report, asJSON)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "run the import again after a run that left its marker")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON")
	return cmd
}

// importDone reports whether an import ran to the end for this account.
func importDone(paths *config.Paths) bool {
	_, err := os.Stat(filepath.Join(paths.DataDir, importMarkerName))
	return err == nil
}

// importUserDatabase reads the account's own database and sends every
// session to the service: the row, then its events, then its receipts.
// The service skips a row it has, so a run that stops leaves a session
// the next run completes.
func importUserDatabase(ctx context.Context, app *App, force bool) (*importReport, error) {
	report := &importReport{Database: app.Config.GetDatabasePath()}
	if !force && importDone(app.Paths) {
		report.Done = true
		report.Note = "already imported, pass --force to run again"
		return report, nil
	}
	if _, err := os.Stat(report.Database); errors.Is(err, os.ErrNotExist) {
		report.Done = true
		report.Note = "no database to import"
		return report, writeImportMarker(app.Paths)
	} else if err != nil {
		return nil, err
	}

	local, err := storage.NewSQLiteStore(report.Database)
	if err != nil {
		return nil, fmt.Errorf("open the database: %w", err)
	}
	defer func() { _ = local.Close() }()
	if err := local.Init(ctx); err != nil {
		return nil, fmt.Errorf("open the database: %w", err)
	}

	socket := app.Config.Supervisor.SocketPath()
	client, err := ipc.Dial(ctx, socket, ipc.DialOptions{Version: version.Version, VerifyServer: func(conn net.Conn) error {
		return hookside.VerifyServer(conn, socket, app.Config.Supervisor.ServerAccount())
	}})
	if err != nil {
		return nil, fmt.Errorf("the decision service at %s: %w", socket, err)
	}
	defer func() { _ = client.Close() }()

	sessions, err := local.QuerySessions(ctx, &session.SessionFilter{})
	if err != nil {
		return nil, err
	}
	for _, sess := range sessions {
		if err := importOneSession(ctx, local, client, sess, report); err != nil {
			return nil, fmt.Errorf("session %s: %w", sess.ID, err)
		}
	}
	report.Done = true
	return report, writeImportMarker(app.Paths)
}

func importOneSession(ctx context.Context, local storage.Store, client *ipc.Client, sess *session.Session, report *importReport) error {
	taken, err := client.Import(ctx, ipc.TypeImportSession, &ipc.ImportSession{Session: sess})
	if err != nil {
		return err
	}
	if taken == 0 {
		report.Skipped++
	}
	report.Sessions += taken
	evts, err := local.GetEventsBySession(ctx, sess.ID)
	if err != nil {
		return err
	}
	for batch := range chunks(evts, ipc.MaxQueryItems) {
		taken, err := client.Import(ctx, ipc.TypeImportEvents, &ipc.ImportEvents{SessionID: sess.ID, Events: batch})
		if err != nil {
			return err
		}
		report.Events += taken
	}
	receipts, err := local.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: &sess.ID, Limit: -1})
	if err != nil {
		return err
	}
	for batch := range chunks(receipts, ipc.MaxQueryItems) {
		taken, err := client.Import(ctx, ipc.TypeImportReceipts, &ipc.ImportReceipts{SessionID: sess.ID, Receipts: batch})
		if err != nil {
			return err
		}
		report.Receipts += taken
	}
	return nil
}

// chunks yields rows in slices of at most size.
func chunks[T any](rows []T, size int) func(yield func([]T) bool) {
	return func(yield func([]T) bool) {
		for start := 0; start < len(rows); start += size {
			end := min(start+size, len(rows))
			if !yield(rows[start:end]) {
				return
			}
		}
	}
}

func writeImportMarker(paths *config.Paths) error {
	if err := os.MkdirAll(paths.DataDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(paths.DataDir, importMarkerName), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
}

func renderImportReport(w io.Writer, report *importReport, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	line := fmt.Sprintf("Imported %d session(s), %d event(s), %d receipt(s) from %s, %d session(s) already there", report.Sessions, report.Events, report.Receipts, report.Database, report.Skipped)
	if report.Note != "" {
		line = report.Note + ": " + report.Database
	}
	if _, err := fmt.Fprintln(w, line); err != nil {
		return err
	}
	log.Debugf("supervisor import: %+v", report)
	return nil
}
