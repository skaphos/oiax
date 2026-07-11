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
  3  converged with reported divergence requiring human attention`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := effectiveConfigRef(cmd, opts)
			if err != nil {
				return err
			}
			g, err := loadGraph(cmd, opts, ref)
			if err != nil {
				return err
			}
			coord, err := buildCoordinator(cmd, g)
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
