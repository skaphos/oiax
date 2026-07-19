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

	"github.com/skaphos/oiax/internal/cienv"
	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/forge/azuredevops"
	"github.com/skaphos/oiax/internal/forge/github"
	"github.com/skaphos/oiax/internal/git"
	"github.com/skaphos/oiax/internal/reconcile"
)

// forgeKind names a forge provider implementation the CLI can wire.
type forgeKind string

const (
	forgeGitHub      forgeKind = "github"
	forgeAzureDevOps forgeKind = "azuredevops"
)

// newForge builds the forge provider a coordinator runs against. It is a
// package variable so tests can substitute a fake; production selects the
// provider (resolveForgeKind) and resolves the repository and token from
// the environment.
var newForge = func(ctx context.Context, logger *slog.Logger) (forge.Forge, error) {
	kind, err := resolveForgeKind(ctx)
	if err != nil {
		return nil, err
	}
	if kind == forgeAzureDevOps {
		repo, rerr := azuredevops.ResolveRepo(ctx)
		if rerr != nil {
			return nil, fmt.Errorf("resolve Azure DevOps repository: %w", rerr)
		}
		return &azuredevops.Provider{
			Repo:  repo,
			Token: os.Getenv("AZURE_DEVOPS_TOKEN"),
			// The Azure Boards work-item type durable conflict artifacts are
			// created as. Empty falls back to the provider default ("Issue");
			// Agile/Scrum/CMMI projects set OIAX_ADO_WORKITEM_TYPE.
			WorkItemType: os.Getenv("OIAX_ADO_WORKITEM_TYPE"),
		}, nil
	}
	owner, repo, err := resolveRepo(ctx)
	if err != nil {
		return nil, err
	}
	return &github.Provider{
		Owner: owner,
		Repo:  repo,
		Token: os.Getenv("GITHUB_TOKEN"),
		// Route the provider's degradation warning through the logger so it
		// is both logged and (under CI) annotated. The token is never
		// part of the warning.
		Warn: func(msg string) { logger.Warn(msg) },
	}, nil
}

// resolveForgeKind selects the forge provider. An explicit OIAX_FORGE
// wins (github or azuredevops; anything else is an error). Otherwise
// detection, cheapest signal first: GITHUB_REPOSITORY (set by the
// Actions runtime) means GitHub; under Azure Pipelines the agent's
// BUILD_REPOSITORY_PROVIDER distinguishes an Azure Repos checkout
// (TfsGit) from a GitHub-hosted one (GitHub); failing both, an origin
// remote that parses as an Azure DevOps URL selects azuredevops. The
// default is github — the first provider, whose own repository
// resolution produces the actionable error when nothing matches.
func resolveForgeKind(ctx context.Context) (forgeKind, error) {
	if v := os.Getenv("OIAX_FORGE"); v != "" {
		switch strings.ToLower(v) {
		case string(forgeGitHub):
			return forgeGitHub, nil
		case string(forgeAzureDevOps):
			return forgeAzureDevOps, nil
		default:
			return "", fmt.Errorf("invalid OIAX_FORGE %q (want github or azuredevops)", v)
		}
	}
	if os.Getenv("GITHUB_REPOSITORY") != "" {
		return forgeGitHub, nil
	}
	switch p := os.Getenv("BUILD_REPOSITORY_PROVIDER"); {
	case strings.EqualFold(p, "TfsGit"):
		return forgeAzureDevOps, nil
	case strings.EqualFold(p, "GitHub"):
		return forgeGitHub, nil
	}
	if out, err := exec.CommandContext(ctx, "git", "remote", "get-url", "origin").Output(); err == nil {
		if _, aerr := azuredevops.ParseRemoteURL(strings.TrimSpace(string(out))); aerr == nil {
			return forgeAzureDevOps, nil
		}
	}
	return forgeGitHub, nil
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
		// Under a detected CI host a shallow clone is a hard error, not a
		// warning: the run is unattended and a spurious promotion PR would land
		// before anyone reads the degraded-mode warning. Locally it stays a
		// warning. This mirrors the pinned-config-ref CI refusal.
		RefuseShallow: cienv.Detect() != cienv.None,
	}, nil
}

// buildLogger builds the structured logger from OIAX_LOG_FORMAT. Structured
// lines and CI annotations both go to stderr, but annotations are only
// emitted when running under a detected CI host — GitHub Actions workflow
// commands or Azure Pipelines logging commands — otherwise they are
// disabled entirely. Annotations must never land on stdout:
// `plan`/`reconcile -o json` write only the JSON plan document there, and
// both hosts scan every captured output line for their command syntax, so
// stderr still surfaces them in the job UI without corrupting a machine
// consumer reading stdout.
func buildLogger(cmd *cobra.Command) *slog.Logger {
	var annOut io.Writer
	style := reconcile.AnnotateGitHub
	switch cienv.Detect() {
	case cienv.GitHubActions:
		annOut = cmd.ErrOrStderr()
	case cienv.AzurePipelines:
		style = reconcile.AnnotateAzurePipelines
		annOut = cmd.ErrOrStderr()
	}
	return reconcile.NewLogger(os.Getenv("OIAX_LOG_FORMAT"), style, annOut, cmd.ErrOrStderr())
}

// renderPlan writes the plan to stdout in the selected output format.
func renderPlan(cmd *cobra.Command, opts *options, plan engine.Plan) error {
	if opts.output == "json" {
		return reconcile.RenderJSON(cmd.OutOrStdout(), plan)
	}
	return reconcile.RenderText(cmd.OutOrStdout(), plan)
}

// writeStepSummary publishes a Markdown rendering of the plan to the CI
// run page: appended to the file named by GITHUB_STEP_SUMMARY when set
// (GitHub Actions), or written to a temp file announced with an
// ##vso[task.uploadsummary] logging command under Azure Pipelines. A
// summary-write failure is reported but never fails the command.
func writeStepSummary(cmd *cobra.Command, plan engine.Plan) {
	if path := os.Getenv("GITHUB_STEP_SUMMARY"); path != "" {
		writeGitHubSummary(cmd, path, plan)
		return
	}
	if cienv.Detect() == cienv.AzurePipelines {
		writeAzureSummary(cmd, plan)
	}
}

func writeGitHubSummary(cmd *cobra.Command, path string, plan engine.Plan) {
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

// writeAzureSummary renders the plan to a Markdown file and emits the
// ##vso[task.uploadsummary] logging command that attaches it to the run
// summary page. Azure hands the step no summary file, so the rendering
// lives in the agent temp directory (cleaned between jobs; the system
// temp dir when unset). The command goes to stderr with the annotations:
// the agent scans every captured line for command syntax, and stdout must
// stay a pure JSON plan for machine consumers.
func writeAzureSummary(cmd *cobra.Command, plan engine.Plan) {
	f, err := os.CreateTemp(os.Getenv("AGENT_TEMPDIRECTORY"), "oiax-summary-*.md")
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "oiax: step summary: %v\n", err)
		return
	}
	renderErr := reconcile.RenderMarkdown(f, plan)
	if cerr := f.Close(); renderErr == nil {
		renderErr = cerr
	}
	if renderErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "oiax: step summary: %v\n", renderErr)
		return
	}
	// A CreateTemp path never contains a newline that could break the
	// single-line command, but the directory is caller-controlled via
	// AGENT_TEMPDIRECTORY, so escape it the way the agent decodes data.
	path := strings.NewReplacer("%", "%AZP25", "\r", "%0D", "\n", "%0A").Replace(f.Name())
	fmt.Fprintf(cmd.ErrOrStderr(), "##vso[task.uploadsummary]%s\n", path)
}

// resolveRepo determines the GitHub owner/repo. GITHUB_REPOSITORY
// (owner/repo, set by the Action runtime) wins; an Azure Pipelines build
// of a GitHub-hosted repository publishes the same pair as
// BUILD_REPOSITORY_NAME; otherwise it is parsed from the origin remote
// URL.
func resolveRepo(ctx context.Context) (owner, repo string, err error) {
	if r := os.Getenv("GITHUB_REPOSITORY"); r != "" {
		o, n, ok := strings.Cut(r, "/")
		if !ok || o == "" || n == "" {
			return "", "", fmt.Errorf("invalid GITHUB_REPOSITORY %q", r)
		}
		return o, n, nil
	}
	// The provider gate matters: for Azure Repos (TfsGit) the variable
	// holds a bare repository name, not owner/repo, and must not be
	// misread. A malformed value falls through to the origin remote.
	if strings.EqualFold(os.Getenv("BUILD_REPOSITORY_PROVIDER"), "github") {
		if o, n, ok := strings.Cut(os.Getenv("BUILD_REPOSITORY_NAME"), "/"); ok && o != "" && n != "" {
			return o, n, nil
		}
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
