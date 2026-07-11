package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/pkg/api/v1alpha1"
)

func newGraphCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "graph",
		Short: "Display the configured promotion topology",
		Long: `Graph validates the configuration and prints the promotion topology:
branches with their roles and drift policies, promotion edges, and the
backflow policy. Output is text; Mermaid and Graphviz renderers are
planned.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			g, err := loadGraph(cmd, opts, opts.configRef)
			if err != nil {
				return err
			}
			printGraph(cmd, g)
			return nil
		},
	}
}

func printGraph(cmd *cobra.Command, g *engine.Graph) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Promotion graph: %s\n\nBranches:\n", g.Name)

	names := make([]string, 0, len(g.Branches))
	for name := range g.Branches {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		b := g.Branches[name]
		annotations := ""
		if b.Role != v1alpha1.RoleNone {
			annotations += string(b.Role)
		}
		if b.Drift == v1alpha1.DriftExpected {
			if annotations != "" {
				annotations += ", "
			}
			annotations += "drift expected"
		}
		if annotations != "" {
			annotations = "  (" + annotations + ")"
		}
		fmt.Fprintf(out, "  %s%s\n", name, annotations)
	}

	fmt.Fprintf(out, "\nPromotions:\n")
	for _, p := range g.Promotions {
		expectation := ""
		if p.Expectations.MergeMethod != "" {
			expectation = fmt.Sprintf("  (expects %s merges)", p.Expectations.MergeMethod)
		}
		fmt.Fprintf(out, "  %s -> %s%s\n", p.From, p.To, expectation)
	}

	if g.Backflow.Enabled {
		fmt.Fprintf(out, "\nBackflow (%s):\n", g.Backflow.Strategy)
		for _, src := range g.Backflow.Sources {
			fmt.Fprintf(out, "  %s -> %s\n", src, g.Backflow.Target)
		}
	}
}
