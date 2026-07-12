package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newValidateCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the promotion graph configuration",
		Long: `Validate parses the configuration file and checks every semantic rule a
promotion graph must satisfy: acyclicity, edge and branch references,
role constraints, and backflow policy consistency.

Repository-state validation (configured branches existing as refs) requires
edge evaluation and is not yet implemented.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireTextOutput("validate", opts); err != nil {
				return err
			}
			g, err := loadGraph(cmd, opts, opts.configRef)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"Configuration valid: graph %q, %d branches, %d promotion edges, backflow %s.\n",
				g.Name, len(g.Branches), len(g.Promotions), enabledWord(g.Backflow.Enabled))
			return nil
		},
	}
}

func enabledWord(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}
