// Package cienv identifies the CI system the current process runs under.
//
// Oiax adapts a small set of behaviors to its CI host: warnings and
// errors are additionally emitted as the host's native annotations, the
// plan summary is published to the run page, and the pinned-config-ref
// fallback to the working tree is refused (a CI run must never silently
// read configuration from the triggering checkout; see ADR 0003). All
// promotion logic is host-independent; nothing here reads credentials.
//
// Detection uses the environment variable each system documents as its
// own marker. It answers "which CI host conventions apply", not "which
// forge holds the repository" — Oiax running under Azure Pipelines
// against a GitHub-hosted repository is a supported combination.
package cienv

import (
	"os"
	"strings"
)

// Kind is the detected CI system.
type Kind int

const (
	// None means no supported CI system was detected: a local run.
	None Kind = iota
	// GitHubActions is detected by GITHUB_ACTIONS=true, which GitHub
	// documents as always set in workflow runs.
	GitHubActions
	// AzurePipelines is detected by TF_BUILD=True, which Azure DevOps
	// documents as set for all pipeline runs.
	AzurePipelines
)

// Detect reports the CI system the process runs under. When both marker
// variables are somehow set, GitHub Actions wins — an arbitrary but
// stable choice; the variables are host-managed and never co-occur on a
// real runner. Comparison is case-insensitive because the two hosts
// document different casings ("true" vs "True").
func Detect() Kind {
	if strings.EqualFold(os.Getenv("GITHUB_ACTIONS"), "true") {
		return GitHubActions
	}
	if strings.EqualFold(os.Getenv("TF_BUILD"), "true") {
		return AzurePipelines
	}
	return None
}
