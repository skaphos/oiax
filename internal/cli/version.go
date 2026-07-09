package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/skaphos/oiax/internal/version"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "oiax %s (commit %s, built %s)\n",
				version.Version, version.Commit, version.Date)
			return nil
		},
	}
}
