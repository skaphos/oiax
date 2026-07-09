package cli

import (
	"fmt"

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
			if _, err := loadGraph(cmd, opts); err != nil {
				return err
			}
			return fmt.Errorf("reconcile: edge evaluation and application are %w", errNotImplemented)
		},
	}
}
