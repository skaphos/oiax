package cli

import (
	"github.com/spf13/cobra"
)

func newReconcileCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "reconcile",
		Short: "Plan, then apply the promotion graph",
		Long: `Reconcile computes the same plan as oiax plan, then applies it: creating
missing managed promotion requests, updating promotion baselines, and
closing obsolete requests. It never merges, approves, force-pushes
long-lived branches, or touches unmanaged requests.

Exit codes (the compatibility contract):
  0  converged (including "applied actions successfully")
  1  error
  3  converged with reported divergence requiring human attention

Exit 3 matches "oiax plan --detailed-exitcode"'s exit 3 for the same state:
both mean a report-only divergence needs a human. Plan's exit 2 (applyable
changes, no divergence) predicts this command applies them and exits 0 —
with one exception plan cannot foresee: a backflow whose commits only
conflict at cherry-pick time surfaces here as exit 3 after a plan of 2.`,
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
			// Render the plan before applying so a failed apply is still
			// explainable from the command's output.
			if err := renderPlan(cmd, opts, plan); err != nil {
				return err
			}
			writeStepSummary(cmd, plan)

			res, err := coord.Apply(cmd.Context(), plan)
			if err != nil {
				return err
			}
			if res.Divergence {
				return exitCodeError{code: 3, msg: "converged with reported divergence"}
			}
			return nil
		},
	}
}
