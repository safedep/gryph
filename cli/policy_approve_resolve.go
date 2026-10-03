package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/approval"
	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

// approveWatchInterval is how often watch asks the service for new
// requests.
const approveWatchInterval = 2 * time.Second

// errApprovalNeedsService says the queue lives in the decision service,
// so a host without it has nothing to resolve.
var errApprovalNeedsService = errors.New("the approval queue lives in the decision service, which is not on this host")

func newPolicyApproveResolveCmd() *cobra.Command {
	var (
		id       string
		decision string
		scope    string
		note     string
		yes      bool
	)
	cmd := &cobra.Command{
		Use:   "resolve",
		Short: "Answer a pending approval request as an approver",
		Long: `Answer a pending approval request of another account.

The decision service checks the answer against the peer credentials of
this command: the account must be in policy.approval.local_admin.group,
and it must not be the account that asked. An answer from the login
session that asked is self-elevated, which the managed configuration
allows or refuses. The first answer wins. A later one is superseded.

An allow stores a grant for the action, bound to the account, the action
digest, the agent session and the scope. The agent retries the action and
the grant lets it through once, for the session, or for the window.

The command confirms on the terminal before it sends an allow. --yes is
refused on a managed host, so an agent with a shell cannot answer.`,
		Example: `  gryph policy approve resolve --id 7f3k2a1b --decision allow --scope once --note "release window"
  gryph policy approve resolve --id 7f3k2a1b --decision deny --note "use the staging target"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if id == "" {
				return ErrConfig("missing --id", fmt.Errorf("--id is required"))
			}
			decision = strings.ToLower(strings.TrimSpace(decision))
			if decision != ipc.ApproveAllow && decision != ipc.ApproveDeny {
				return ErrConfig("invalid --decision", fmt.Errorf("--decision must be allow or deny"))
			}
			if scope != "" {
				if _, err := approval.ParseScope(scope); err != nil {
					return ErrConfig("invalid --scope", err)
				}
			}
			ctx := context.Background()
			app, err := loadApp()
			if err != nil {
				return err
			}
			if !clientMode(app.Config) {
				return ErrConfig("no approval queue", errApprovalNeedsService)
			}
			if yes {
				return ErrConfig("--yes is refused", fmt.Errorf("an approver confirms on the terminal on a managed host"))
			}
			if err := app.InitReadStore(ctx); err != nil {
				return ErrDatabase("failed to reach the decision service", err)
			}
			defer func() {
				if cerr := app.Close(); cerr != nil {
					log.Errorf("failed to close app: %v", cerr)
				}
			}()
			row, err := app.Reads.GetApprovalRequestByPrefix(ctx, id)
			if err != nil {
				return fmt.Errorf("failed to find the approval request: %w", err)
			}
			if row == nil {
				return ErrConfig("no approval request matches id", fmt.Errorf("id %q did not match any request", id))
			}
			if row.State != storage.ApprovalRequestPending {
				return ErrConfig("request already decided", fmt.Errorf("request %s is %s", tui.FormatShortID(row.ID.String()), row.State))
			}
			out := cmd.OutOrStdout()
			c := policyColorizer(app)
			renderApprovalRequest(out, c, row)
			confirmed, err := confirmOnTerminal(fmt.Sprintf("Answer request %s with %s? [y/N]: ", tui.FormatShortID(row.ID.String()), strings.ToUpper(decision)))
			if err != nil {
				return err
			}
			if !confirmed {
				_, _ = fmt.Fprintf(out, "Aborted: request %s not answered\n", tui.FormatShortID(row.ID.String()))
				return nil
			}
			res, err := app.Approve(ctx, ipc.Approve{RequestID: row.ID, Decision: decision, Scope: scope, Note: note})
			if err != nil {
				var se *ipc.ServerError
				if errors.As(err, &se) && se.Code == ipc.CodeUnauthorized {
					return NewCLIError(ExitGeneral, "the decision service refused the answer: "+se.Message)
				}
				return WrapError(ExitGeneral, "answer the request", err)
			}
			switch res.State {
			case "superseded":
				_, _ = fmt.Fprintf(out, "%s Request %s was answered first by someone else: %s\n", c.StatusFail(), c.Cyan(tui.FormatShortID(res.RequestID.String())), res.Note)
				return NewCLIError(ExitGeneral, "the answer was superseded")
			default:
				line := fmt.Sprintf("%s Request %s is %s (%s)", c.StatusOK(), c.Cyan(tui.FormatShortID(res.RequestID.String())), c.Success(res.State), res.Assurance)
				if res.GrantID != "" {
					line += fmt.Sprintf(", grant %s", tui.FormatShortID(res.GrantID))
				}
				_, _ = fmt.Fprintln(out, line)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "approval request id or id prefix (required)")
	cmd.Flags().StringVar(&decision, "decision", "", "answer: allow or deny (required)")
	cmd.Flags().StringVar(&scope, "scope", "", "scope of the grant an allow stores: once (default), session or window")
	cmd.Flags().StringVar(&note, "note", "", "note recorded on the request and the receipt")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation on the terminal (refused on a managed host)")
	return cmd
}

// openTerminal opens the controlling terminal of the command. Tests
// replace it.
var openTerminal = func() (io.ReadWriteCloser, error) {
	return os.OpenFile("/dev/tty", os.O_RDWR, 0)
}

// confirmOnTerminal asks one yes or no question on the controlling
// terminal, never on stdin, so an answer has to come from a person at the
// terminal. Without a terminal the answer is no.
func confirmOnTerminal(question string) (bool, error) {
	tty, err := openTerminal()
	if err != nil {
		return false, NewCLIError(ExitGeneral, "an approver answers on a terminal, and this command has none: "+err.Error())
	}
	defer func() { _ = tty.Close() }()
	if _, err := fmt.Fprint(tty, question); err != nil {
		return false, err
	}
	line, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("failed to read the confirmation: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func newPolicyApproveWatchCmd() *cobra.Command {
	var interval time.Duration
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Print each new approval request as it arrives, until Ctrl-C",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if interval < time.Second {
				return ErrConfig("invalid --interval", fmt.Errorf("--interval must be 1s or more"))
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			app, err := loadApp()
			if err != nil {
				return err
			}
			if err := app.InitReadStore(ctx); err != nil {
				return ErrDatabase("failed to open database", err)
			}
			defer func() {
				if cerr := app.Close(); cerr != nil {
					log.Errorf("failed to close app: %v", cerr)
				}
			}()
			out := cmd.OutOrStdout()
			c := policyColorizer(app)
			seen := map[string]bool{}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				rows, err := app.Reads.QueryApprovalRequests(ctx, &storage.ApprovalRequestFilter{State: storage.ApprovalRequestPending, AllAccounts: true})
				if err != nil {
					return fmt.Errorf("failed to query approval requests: %w", err)
				}
				for i := len(rows) - 1; i >= 0; i-- {
					r := rows[i]
					if seen[r.ID.String()] {
						continue
					}
					seen[r.ID.String()] = true
					_, _ = fmt.Fprintln(out, formatApprovalRequestLine(c, r))
				}
				select {
				case <-ctx.Done():
					return nil
				case <-ticker.C:
				}
			}
		},
	}
	cmd.Flags().DurationVar(&interval, "interval", approveWatchInterval, "time between two reads of the queue")
	return cmd
}

func formatApprovalRequestLine(c *tui.Colorizer, r *storage.ApprovalRequestRow) string {
	rule := ""
	if len(r.RuleIDs) > 0 {
		rule = r.RuleIDs[0]
	}
	return fmt.Sprintf("%s  %s  %s  %s  %s  %s",
		r.RequestedAt.Local().Format("15:04:05"),
		c.Cyan(tui.FormatShortID(r.ID.String())),
		r.Requester, r.Agent, rule, r.Summary)
}
