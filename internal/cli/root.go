// Package cli wires the Cobra command tree. The CLI is the canonical
// product interface; the GitHub Action is a thin wrapper around this
// binary and contains no promotion logic of its own.
package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/skaphos/oiax/internal/config"
	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/git"
	"github.com/skaphos/oiax/pkg/api/v1alpha1"
)

// errNotImplemented distinguishes roadmap gaps from real failures in
// command output.
var errNotImplemented = errors.New("not implemented in this development snapshot; see the roadmap in docs/architecture.md")

// options are the persistent flags shared by every command.
type options struct {
	// configPath is the repository-local configuration file.
	configPath string
	// configRef pins the ref configuration is read from via
	// `git show <ref>:<path>`. Empty means the working-tree file at
	// configPath (resolving the repository default branch automatically
	// is roadmap scope). Reading configuration from whatever ref
	// triggered a run would make behavior depend on which branch moved
	// last, and would execute untrusted pull-request configuration with
	// privileged credentials.
	configRef string
	// output selects text or json output for plan-producing commands.
	output string
}

// NewRootCommand builds the oiax command tree.
func NewRootCommand() *cobra.Command {
	opts := &options{}

	root := &cobra.Command{
		Use:   "oiax",
		Short: "Declarative Git branch promotion reconciler",
		Long: `Oiax reconciles branch promotion pull requests for
branch-per-environment GitOps repositories.

Given a promotion graph declared in .oiax.yaml, Oiax observes branch and
forge state and ensures the pull requests required to move changes through
that graph exist — exactly one active managed request per diverged edge,
no duplicates, no stale leftovers. It never merges, approves, or deploys.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.DisableAutoGenTag = true

	pf := root.PersistentFlags()
	pf.StringVar(&opts.configPath, "config", config.DefaultPath, "path to the PromotionGraph configuration file")
	pf.StringVar(&opts.configRef, "config-ref", "", "ref to read configuration from via git (default: the working-tree file)")
	pf.StringVarP(&opts.output, "output", "o", "text", "output format: text or json")

	root.AddCommand(
		newValidateCommand(opts),
		newPlanCommand(opts),
		newReconcileCommand(opts),
		newGraphCommand(opts),
		newVersionCommand(),
		newGenCommand(),
	)
	return root
}

// loadGraph loads, converts, and semantically validates the configured
// promotion graph, reporting every violation at once. With --config-ref
// the file is read as committed at that ref (the pinned-ref rule);
// otherwise the working-tree file is read.
func loadGraph(cmd *cobra.Command, opts *options) (*engine.Graph, error) {
	var cfg *v1alpha1.PromotionGraph
	var err error
	if opts.configRef != "" {
		runner := &git.Runner{}
		var data []byte
		data, err = runner.ShowFile(cmd.Context(), opts.configRef, opts.configPath)
		if err != nil {
			return nil, err
		}
		cfg, err = config.Parse(data)
		if err != nil {
			err = fmt.Errorf("%s at ref %s: %w", opts.configPath, opts.configRef, err)
		}
	} else {
		cfg, err = config.Load(opts.configPath)
	}
	if err != nil {
		return nil, err
	}
	g := engine.FromConfig(cfg)
	if violations := g.Validate(); len(violations) > 0 {
		for _, v := range violations {
			fmt.Fprintf(cmd.ErrOrStderr(), "invalid: %v\n", v)
		}
		return nil, fmt.Errorf("%s: %d validation errors", opts.configPath, len(violations))
	}
	return g, nil
}

// Execute runs the CLI and returns a process exit code.
func Execute(args []string) int {
	root := NewRootCommand()
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		fmt.Fprintf(root.ErrOrStderr(), "oiax: %v\n", err)
		return 1
	}
	return 0
}
