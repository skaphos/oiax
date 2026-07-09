package cli

import (
	"fmt"

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
  2  valid plan with pending actions (only with --detailed-exitcode)`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := loadGraph(cmd, opts); err != nil {
				return err
			}
			return fmt.Errorf("plan: edge evaluation is %w", errNotImplemented)
		},
	}
	cmd.Flags().BoolVar(&detailedExitCode, "detailed-exitcode", false, "exit 2 when the plan contains pending actions")
	return cmd
}
