// Package cli wires the Cobra command tree. The CLI is the canonical
// product interface; the GitHub Action and the Azure Pipelines template
// are thin wrappers around this binary and contain no promotion logic of
// their own.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/skaphos/oiax/internal/cienv"
	"github.com/skaphos/oiax/internal/config"
	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/git"
	"github.com/skaphos/oiax/internal/tmpl"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
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
	// `git show <ref>:<path>`. When empty, plan and reconcile default to
	// the repository default branch (origin/HEAD) and the inspection
	// commands (validate, graph) read the working-tree file. Reading
	// configuration from whatever ref triggered a run would make behavior
	// depend on which branch moved last, and would execute untrusted
	// pull-request configuration with privileged credentials (ADR 0003).
	configRef string
	// output selects text or json output for plan-producing commands.
	output string
}

// NewRootCommand builds the oiax command tree.
func NewRootCommand() *cobra.Command {
	opts := &options{}
	var showVersion bool

	root := &cobra.Command{
		Use:   "oiax",
		Short: "Declarative Git branch promotion reconciler",
		Long: `Oiax reconciles branch promotion pull requests for
branch-per-environment GitOps repositories.

Given a promotion graph declared in .oiax.yaml, Oiax observes branch and
forge state and ensures the pull requests required to move changes through
that graph exist — exactly one active managed request per diverged edge,
with no duplicates. Requests for edges removed from the graph are left for a
human to close. Oiax never merges, approves, or deploys.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		// PersistentPreRunE runs for every command in the tree (no
		// subcommand overrides it), so an invalid --output value is
		// rejected uniformly instead of each command re-checking it.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			switch opts.output {
			case "text", "json":
				return nil
			default:
				return fmt.Errorf("invalid --output %q: want %q or %q", opts.output, "text", "json")
			}
		},
		// RunE only fires when oiax is invoked with no subcommand: `oiax
		// --version` prints version information (mirroring the `version`
		// subcommand); bare `oiax` falls back to the usual help text.
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				return printVersion(cmd, opts)
			}
			// Bare `oiax` prints help, which has no JSON rendering; reject a
			// non-text --output rather than silently ignore it, mirroring
			// requireTextOutput on validate/graph.
			if err := requireTextOutput("oiax", opts); err != nil {
				return err
			}
			return cmd.Help()
		},
	}
	root.DisableAutoGenTag = true

	pf := root.PersistentFlags()
	pf.StringVar(&opts.configPath, "config", config.DefaultPath, "path to the PromotionGraph configuration file")
	pf.StringVar(&opts.configRef, "config-ref", "", "ref to read configuration from via 'git show' (default: the repository default branch for plan/reconcile, the working-tree file for validate/graph)")
	pf.StringVarP(&opts.output, "output", "o", "text", "output format: text or json")
	root.Flags().BoolVar(&showVersion, "version", false, "print version information and exit")

	root.AddCommand(
		newValidateCommand(opts),
		newPlanCommand(opts),
		newReconcileCommand(opts),
		newGraphCommand(opts),
		newVersionCommand(opts),
		newGenCommand(),
	)
	return root
}

// requireTextOutput rejects --output on commands that have no alternative
// rendering. validate and graph print human-readable status only: there is
// no JSON shape for them (yet), so silently ignoring an unsupported
// --output value is worse than a clear rejection at the flag boundary.
func requireTextOutput(cmdName string, opts *options) error {
	if opts.output != "text" {
		return fmt.Errorf("%s: --output %q is not supported; %s only prints text", cmdName, opts.output, cmdName)
	}
	return nil
}

// loadGraph loads, converts, and semantically validates the promotion
// graph, reporting every violation at once, and resolves the configured
// template set (SKA-54) alongside it. configRef selects the source: a
// non-empty ref is read as committed at that ref (git show <ref>:<path>),
// the pinned-configuration-ref rule; an empty configRef reads the
// working-tree file at configPath. Template file references (bodyFile /
// file) are read from the SAME source as the document itself, so pull-
// request template content is never executed with privileged credentials
// (ADR 0003), and a broken template fails here — in validate's round trip —
// not deep inside an apply.
func loadGraph(cmd *cobra.Command, opts *options, configRef string) (*engine.Graph, *tmpl.Set, error) {
	var cfg *v1.PromotionGraph
	var err error
	runner := &git.Runner{}
	if configRef != "" {
		var data []byte
		data, err = runner.ShowFile(cmd.Context(), configRef, opts.configPath)
		if err != nil {
			return nil, nil, err
		}
		cfg, err = config.Parse(data)
		if err != nil {
			err = fmt.Errorf("%s at ref %s: %w", opts.configPath, configRef, err)
		}
	} else {
		cfg, err = config.Load(opts.configPath)
	}
	if err != nil {
		return nil, nil, err
	}
	if config.IsDeprecatedAPIVersion(cfg.APIVersion) {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: apiVersion %q is deprecated; migrate to %q\n", v1.APIVersionV1Alpha1, v1.APIVersion)
	}
	// The canonical semantic rules live on the public document type
	// (pkg/api/v1), so `oiax validate` and an external Go integrator
	// calling cfg.Validate() run the identical checks. Conversion to the
	// engine model happens only after the document is known valid.
	if violations := cfg.Validate(); len(violations) > 0 {
		for _, v := range violations {
			fmt.Fprintf(cmd.ErrOrStderr(), "invalid: %v\n", v)
		}
		return nil, nil, fmt.Errorf("%s: %d validation errors", opts.configPath, len(violations))
	}
	ts, err := tmpl.Resolve(cfg, func(path string) ([]byte, error) {
		if configRef != "" {
			return runner.ShowFile(cmd.Context(), configRef, path)
		}
		return readWorkingTreeFile(path)
	})
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", opts.configPath, err)
	}
	return engine.FromConfig(cfg), ts, nil
}

// readWorkingTreeFile reads a repository-relative template file from the
// working tree, capped just past the template size limit so a pathological
// file is rejected by Resolve's bound rather than read into memory whole —
// mirroring config.Load's own cap.
func readWorkingTreeFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, tmpl.MaxFileBytes+1))
}

// effectiveConfigRef resolves the ref plan and reconcile read configuration
// from, enforcing the pinned-configuration-ref rule (ADR 0003) so a run
// never depends on the branch that triggered it. An explicit --config-ref
// wins. Otherwise the repository default branch (origin/HEAD) is used. When
// the default branch cannot be resolved — a remote-less or shallow checkout
// with no origin/HEAD — a local run falls back to the working-tree file
// (empty ref), but a run under CI (GitHub Actions or Azure Pipelines)
// refuses: silently reading the checked-out ref there would execute
// untrusted pull-request configuration with privileged credentials. The
// operator pins --config-ref to recover.
func effectiveConfigRef(cmd *cobra.Command, opts *options) (string, error) {
	if opts.configRef != "" {
		return opts.configRef, nil
	}
	runner := &git.Runner{}
	if ref, ok := runner.DefaultBranchRef(cmd.Context()); ok {
		return ref, nil
	}
	if cienv.Detect() != cienv.None {
		return "", fmt.Errorf("cannot resolve the repository default branch (origin/HEAD is not set); pin --config-ref to the default branch, for example --config-ref origin/main")
	}
	return "", nil
}

// Execute runs the CLI and returns a process exit code. An exitCodeError
// carries its own code (and an optional message); any other error is the
// generic failure path (exit 1); nil is success (exit 0). The context is
// threaded to every command via cobra's ExecuteContext, so a caller that
// cancels it (main installs a SIGINT/SIGTERM handler) cancels the in-flight
// git subprocess and any Retry-After backoff.
func Execute(ctx context.Context, args []string) int {
	root := NewRootCommand()
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
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
