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

// exitCodeError lets a command request a specific process exit code
// without the generic "oiax: <err>" framing. It is the only way exit codes
// 2 (plan has pending actions) and 3 (reconcile converged with reported
// divergence) reach Execute's single return-code path. Execute unwraps it
// with errors.As.
type exitCodeError struct {
	// code is the process exit code to return.
	code int
	// msg is printed to stderr when non-empty; empty means a silent status
	// code (e.g. plan --detailed-exitcode).
	msg string
}

func (e exitCodeError) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return fmt.Sprintf("exit code %d", e.code)
}

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

// Execute runs the CLI and returns a process exit code. An exitCodeError
// carries its own code (and an optional message); any other error is the
// generic failure path (exit 1); nil is success (exit 0).
func Execute(args []string) int {
	root := NewRootCommand()
	root.SetArgs(args)
	err := root.Execute()
	if err == nil {
		return 0
	}
	var ece exitCodeError
	if errors.As(err, &ece) {
		if ece.msg != "" {
			fmt.Fprintln(root.ErrOrStderr(), ece.msg)
		}
		return ece.code
	}
	fmt.Fprintf(root.ErrOrStderr(), "oiax: %v\n", err)
	return 1
}
