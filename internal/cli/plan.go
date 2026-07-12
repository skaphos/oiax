package cli

import (
	"github.com/spf13/cobra"
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
  2  valid plan with pending actions (only with --detailed-exitcode)

Exit 2 fires for ANY pending action, including a report-only divergence
that reconcile cannot auto-resolve (see "oiax reconcile --help"). A gate
that treats "plan exit 2" as "reconcile will converge to exit 0" is wrong:
running reconcile against that same state can still exit 3. Plan's 2 means
"there is something to do"; reconcile's 3 means "reconcile did what it
could and something still needs a human." Do not conflate the two codes
across commands.`,
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
			g, err := loadGraph(cmd, opts, ref)
			if err != nil {
				return err
			}
			coord, err := buildCoordinator(cmd, g, runner)
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

			// Every emitted action is non-NoOp, so any action means the graph
			// is not fully in sync. With --detailed-exitcode that is exit 2
			// (silent); otherwise a successful plan is exit 0.
			if len(plan.Actions) > 0 && detailedExitCode {
				return exitCodeError{code: 2}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&detailedExitCode, "detailed-exitcode", false, "exit 2 when the plan contains pending actions")
	return cmd
}
