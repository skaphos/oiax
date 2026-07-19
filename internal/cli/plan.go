package cli

import (
	"github.com/spf13/cobra"

	"github.com/skaphos/oiax/internal/engine"
)

func newPlanCommand(opts *options) *cobra.Command {
	var detailedExitCode bool

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Compute the actions required to converge the promotion graph",
		Long: `Plan observes branch and forge state, evaluates every promotion edge
through the equivalence ladder, and prints the actions reconcile would
apply — without applying anything. Plan is the dry run; there is no
separate dry-run flag.

Exit codes (the compatibility contract, following terraform plan):
  0  fully in sync (or, without --detailed-exitcode, any successful plan)
  1  error
  2  applyable changes pending (only with --detailed-exitcode)
  3  a report-only divergence is present (only with --detailed-exitcode)

With --detailed-exitcode the exit code predicts what "oiax reconcile" does
for the same state: 2 means reconcile applies the pending changes and
converges to its exit 0; 3 means the plan already contains a report-only
divergence that reconcile will surface and exit 3 on, matching reconcile's
own exit 3. A gate may therefore treat plan's 2 as "safe to reconcile" and
3 as "needs a human." One residual reconcile alone can see: a backflow
whose commits only conflict when cherry-picked shows here as an applyable
change (exit 2) — plan cannot foresee the conflict — so reconcile can still
exit 3 after a plan of 2 in that single case.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Assert the git version floor before any other git subprocess
			// (config resolution, graph load) so an unsupported runner fails
			// fast with a clear message, not a raw git error mid-flight.
			runner, err := requireGitFloor(cmd)
			if err != nil {
				return err
			}
			ref, err := effectiveConfigRef(cmd, opts)
			if err != nil {
				return err
			}
			g, ts, err := loadGraph(cmd, opts, ref)
			if err != nil {
				return err
			}
			coord, err := buildCoordinator(cmd, g, ts, runner)
			if err != nil {
				return err
			}
			plan, err := coord.Plan(cmd.Context())
			if err != nil {
				return err
			}
			if err := renderPlan(cmd, opts, plan); err != nil {
				return err
			}
			writeStepSummary(cmd, plan)

			// With --detailed-exitcode, return a status code that predicts
			// reconcile's outcome for this state (see the command help): 3 for a
			// report-only divergence, 2 for other applyable changes, 0 in sync.
			// Without the flag, a successful plan is always exit 0.
			if detailedExitCode {
				if code := planExitCode(plan); code != 0 {
					return exitCodeError{code: code}
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&detailedExitCode, "detailed-exitcode", false, "exit 2 for applyable changes, 3 for a report-only divergence")
	return cmd
}

// planReportsDivergence reports whether the plan contains a report-only
// divergence — the ActionReportDivergence the planner emits for a non-backflow
// destination with unexpected downstream content. When it is present, reconcile
// also sets res.Divergence and exits 3 for the same state. The converse does
// not hold: reconcile can also exit 3 for an apply-time backflow cherry-pick
// conflict the plan cannot foresee (see the command help).
func planReportsDivergence(plan engine.Plan) bool {
	for _, a := range plan.Actions {
		if a.Type == engine.ActionReportDivergence {
			return true
		}
	}
	return false
}

// planExitCode maps a plan to its --detailed-exitcode result, chosen so the
// code predicts what reconcile does for the same state: 3 when a report-only
// divergence is present (reconcile exits 3), 2 when other applyable changes are
// pending (reconcile applies them and exits 0), and 0 when fully in sync. Every
// emitted action is non-NoOp, so a non-empty plan always means work.
func planExitCode(plan engine.Plan) int {
	if planReportsDivergence(plan) {
		return 3
	}
	if len(plan.Actions) > 0 {
		return 2
	}
	return 0
}
