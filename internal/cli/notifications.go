package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/skaphos/oiax/v2/internal/notification"
)

// newNotificationsCommand groups the notification maintenance commands. Nothing
// here is part of a scheduled run: plan and reconcile own the normal lifecycle,
// and these exist only for states an operator has to decide about.
func newNotificationsCommand(opts *options) *cobra.Command {
	group := &cobra.Command{
		Use:   "notifications",
		Short: "Inspect and repair notification delivery state",
		Long: `Notification maintenance commands.

Notification delivery state lives in an append-only Git notes ledger, not in a
private database. plan and reconcile keep it current on their own; the commands
here exist for the states that need a human decision and have no safe automatic
answer.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireTextOutput("notifications", opts); err != nil {
				return err
			}
			return cmd.Help()
		},
	}
	group.AddCommand(newNotificationsResetCommand(opts))
	return group
}

// notificationResetResult is the machine rendering of a reset. The nil-record
// case is reported explicitly rather than as an empty document, so a caller
// can tell "already accepted, nothing done" from "recovery performed".
type notificationResetResult struct {
	Reset       bool   `json:"reset"`
	PriorOID    string `json:"priorOID,omitempty"`
	AcceptedOID string `json:"acceptedOID,omitempty"`
	RecordedAt  string `json:"recordedAt,omitempty"`
}

func newNotificationsResetCommand(opts *options) *cobra.Command {
	var acceptRevision string
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Accept the pinned configuration revision when the accepted one is gone",
		Long: `Reset records an operator-authorized acceptance of the pinned configuration
revision for a ledger whose accepted revision names a commit that no longer
exists in the repository.

Oiax only advances notification policy onto a revision it can prove is a
descendant of the accepted one. When the configuration branch is force-pushed,
rewritten or garbage-collected, the accepted commit disappears, "git merge-base
--is-ancestor" can no longer answer, and every run defers with
config-revision-unreachable. The ordinary fix — commit a reviewed descendant —
is impossible, because there is nothing left to descend from, and deleting the
notes ref would destroy the receipts that prevent duplicate sends.

This command is the sanctioned way out, and it is deliberately narrow:

  * It refuses while the accepted commit is still resolvable. Ordering is
    decidable there, so the ordinary rule applies and this is not a general
    way around it.
  * It refuses in a shallow repository. A shallow or partial checkout is
    missing objects the remote still has, so a missing commit is not evidence
    the commit is gone. Fetch full history first and confirm.
  * --accept-revision must name the configuration commit this invocation
    resolved, so the acceptance is an explicit act and cannot be a hard-coded
    step in a workflow that silently keeps working as configuration moves.
  * It never deletes, resets or backfills anything. Events, deliveries and
    delivery receipts are carried across untouched.
  * It appends an immutable record of the override to the ledger, naming the
    abandoned commit and the accepted one, so the gap in the ordering chain
    stays auditable forever.

Re-running it after it has succeeded is a no-op.

To accept a revision other than the repository default branch's head, pin it:

  oiax notifications reset --config-ref <sha> --accept-revision <sha>`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := requireGitFloor(cmd)
			if err != nil {
				return err
			}
			ref, err := effectiveConfigRef(cmd, opts)
			if err != nil {
				return err
			}
			loaded, err := loadGraph(cmd, opts, ref)
			if err != nil {
				return err
			}
			// The recorded revision, the policy digest and the accepted OID must
			// all come from one reviewed commit; a working-tree read has no OID
			// to record and could not be reproduced by a later run.
			if loaded.ConfigOID == "" {
				return errors.New("notifications reset: configuration must be read from a commit; pin --config-ref, for example --config-ref origin/main")
			}
			if !loaded.Notifications.IsEnabled() {
				return errors.New("notifications reset: notifications are not enabled in the pinned configuration; a fully disabled policy performs no ledger operation at all")
			}
			if acceptRevision != loaded.ConfigOID {
				return fmt.Errorf("notifications reset: --accept-revision %q does not name the resolved configuration commit %s; pass that value to confirm the revision you are accepting", acceptRevision, loaded.ConfigOID)
			}
			// The whole recovery rests on "this commit is gone", and a shallow
			// clone makes that observation meaningless.
			shallow, err := runner.IsShallowRepository(cmd.Context())
			if err != nil {
				return err
			}
			if shallow {
				return errors.New("notifications reset: this is a shallow clone, so a missing commit is not evidence that the commit is gone; fetch full history (git fetch --unshallow) and re-run")
			}
			coord, err := buildCoordinator(cmd, loaded, runner)
			if err != nil {
				return err
			}
			record, err := coord.ResetNotificationRevision(cmd.Context())
			if err != nil {
				return notificationResetError(err, loaded.ConfigOID)
			}
			for _, d := range coord.NotificationDiagnostics {
				coord.Log.Warn("notification revision reset", "scope", d.Scope(), "reason", d.Reason, "action", d.Action)
			}
			writeNotificationSummary(cmd, coord.NotificationDiagnostics)
			return renderNotificationReset(cmd, opts, record)
		},
	}
	cmd.Flags().StringVar(&acceptRevision, "accept-revision", "", "the configuration commit to accept; must equal the commit --config-ref resolves to")
	// The only error here is a misspelled flag name, which the wiring test
	// would catch; the flag is declared immediately above.
	_ = cmd.MarkFlagRequired("accept-revision")
	return cmd
}

// notificationResetError explains the refusals in the operator's terms while
// keeping the sentinel wrapped, so callers and tests still match on it. Object
// ids are safe to print; nothing here formats a provider or transport error.
func notificationResetError(err error, configOID string) error {
	switch {
	case errors.Is(err, notification.ErrRevisionReachable):
		return fmt.Errorf("notifications reset: the accepted configuration commit is present in this repository, so its ordering against %s is decidable; commit a reviewed descendant instead: %w", configOID, err)
	case errors.Is(err, notification.ErrAbsent):
		return fmt.Errorf("notifications reset: there is no notification ledger to repair; the next reconcile establishes a cutoff: %w", err)
	}
	return fmt.Errorf("notifications reset: %w", err)
}

func renderNotificationReset(cmd *cobra.Command, opts *options, record *notification.RevisionOverrideV1) error {
	result := notificationResetResult{Reset: record != nil}
	if record != nil {
		result.PriorOID, result.AcceptedOID = record.PriorOID, record.AcceptedOID
		result.RecordedAt = record.RecordedAt.UTC().Format(time.RFC3339Nano)
	}
	if opts.output == "json" {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	if record == nil {
		fmt.Fprintln(cmd.OutOrStdout(), "notifications reset: the ledger already accepts the pinned configuration revision; nothing to do")
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "notifications reset: accepted %s in place of the unreachable %s\n", result.AcceptedOID, result.PriorOID)
	fmt.Fprintln(cmd.OutOrStdout(), "The override is recorded in the notification ledger. Delivery receipts were preserved.")
	return nil
}
