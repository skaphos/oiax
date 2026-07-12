package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/forge/github"
	"github.com/skaphos/oiax/internal/git"
	"github.com/skaphos/oiax/internal/reconcile"
)

// newForge builds the forge provider a coordinator runs against. It is a
// package variable so tests can substitute a fake; production resolves the
// GitHub repository and token from the environment.
var newForge = func(ctx context.Context, logger *slog.Logger) (forge.Forge, error) {
	owner, repo, err := resolveRepo(ctx)
	if err != nil {
		return nil, err
	}
	return &github.Provider{
		Owner: owner,
		Repo:  repo,
		Token: os.Getenv("GITHUB_TOKEN"),
		// Route the provider's degradation warning through the logger so it
		// is both logged and (under Actions) annotated. The token is never
		// part of the warning.
		Warn: func(msg string) { logger.Warn(msg) },
	}, nil
}

// requireGitFloor asserts the system git satisfies oiax's version floor before
// any other git-dependent work runs, and returns the Runner it checked so the
// same instance can back the coordinator. Backflow replay needs cherry-pick
// --empty=drop (git >= 2.45), so an unsupported runner must fail fast with a
// clear up-front message rather than surface a raw git error deep inside config
// resolution or a reconcile. It is the first action of plan and reconcile, ahead
// of effectiveConfigRef and loadGraph, so the floor is asserted before any other
// git subprocess (DefaultBranchRef, ShowFile) is spawned.
func requireGitFloor(cmd *cobra.Command) (*git.Runner, error) {
	runner := &git.Runner{}
	if err := runner.RequireMinVersion(cmd.Context()); err != nil {
		return nil, err
	}
	return runner, nil
}

// buildCoordinator assembles the coordinator for a plan-producing command:
// the structured logger, the forge provider, and the git runner over the
// working directory. runner is the instance requireGitFloor already asserted
// the version floor on, so the floor is checked exactly once.
func buildCoordinator(cmd *cobra.Command, g *engine.Graph, runner *git.Runner) (*reconcile.Coordinator, error) {
	logger := buildLogger(cmd)
	f, err := newForge(cmd.Context(), logger)
	if err != nil {
		return nil, err
	}
	return &reconcile.Coordinator{
		Git:   runner,
		Forge: f,
		Graph: g,
		Log:   logger,
	}, nil
}

// buildLogger builds the structured logger from OIAX_LOG_FORMAT. Structured
// lines and GitHub Actions annotations both go to stderr, but annotations
// are only emitted when running under Actions (GITHUB_ACTIONS=true) —
// otherwise they are disabled entirely. Annotations must never land on
// stdout: `plan`/`reconcile -o json` write only the JSON plan document
// there, and the Actions runner scans both stdout and stderr for
// `::warning::`/`::error::` workflow commands, so stderr still surfaces
// them in the job UI without corrupting a machine consumer reading stdout.
func buildLogger(cmd *cobra.Command) *slog.Logger {
	var annOut io.Writer
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		annOut = cmd.ErrOrStderr()
	}
	return reconcile.NewLogger(os.Getenv("OIAX_LOG_FORMAT"), annOut, cmd.ErrOrStderr())
}

// renderPlan writes the plan to stdout in the selected output format.
func renderPlan(cmd *cobra.Command, opts *options, plan engine.Plan) error {
	if opts.output == "json" {
		return reconcile.RenderJSON(cmd.OutOrStdout(), plan)
	}
	return reconcile.RenderText(cmd.OutOrStdout(), plan)
}

// writeStepSummary appends a Markdown rendering of the plan to the file
// named by GITHUB_STEP_SUMMARY, when set. A summary-write failure is
// reported but never fails the command.
func writeStepSummary(cmd *cobra.Command, plan engine.Plan) {
	path := os.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "oiax: step summary: %v\n", err)
		return
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "oiax: step summary: %v\n", cerr)
		}
	}()
	if err := reconcile.RenderMarkdown(f, plan); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "oiax: step summary: %v\n", err)
	}
}

// resolveRepo determines the GitHub owner/repo. GITHUB_REPOSITORY
// (owner/repo, set by the Action runtime) wins; otherwise it is parsed from
// the origin remote URL.
func resolveRepo(ctx context.Context) (owner, repo string, err error) {
	if r := os.Getenv("GITHUB_REPOSITORY"); r != "" {
		o, n, ok := strings.Cut(r, "/")
		if !ok || o == "" || n == "" {
			return "", "", fmt.Errorf("invalid GITHUB_REPOSITORY %q", r)
		}
		return o, n, nil
	}
	out, err := exec.CommandContext(ctx, "git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", "", fmt.Errorf("resolve repository from origin remote: %w", err)
	}
	return parseRemoteURL(strings.TrimSpace(string(out)))
}

// parseRemoteURL extracts owner and repo from a GitHub remote URL in the
// common forms: git@github.com:owner/repo.git, https://github.com/owner/
// repo.git, and ssh://git@github.com/owner/repo.git.
func parseRemoteURL(remote string) (owner, repo string, err error) {
	s := strings.TrimSuffix(remote, ".git")
	// Normalize the scp-like SSH form to a slash-delimited path.
	if i := strings.LastIndex(s, ":"); i >= 0 && !strings.Contains(s, "://") {
		s = s[i+1:]
	} else {
		if j := strings.Index(s, "://"); j >= 0 {
			s = s[j+3:]
		}
		if k := strings.Index(s, "/"); k >= 0 {
			s = s[k+1:]
		}
	}
	s = strings.Trim(s, "/")
	parts := strings.Split(s, "/")
	if len(parts) < 2 || parts[len(parts)-2] == "" || parts[len(parts)-1] == "" {
		return "", "", fmt.Errorf("cannot parse owner/repo from remote %q", remote)
	}
	return parts[len(parts)-2], parts[len(parts)-1], nil
}
