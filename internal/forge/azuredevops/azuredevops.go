// Package azuredevops is the Azure Repos forge provider (ADR 0009):
// Provider implements forge.Forge over the Azure DevOps REST API —
// managed pull requests, oiax/ branch pushes and deletes, merge-strategy
// policy discovery, and durable conflict artifacts as Azure Boards work
// items. This file owns repository identity — the
// organization/project/repository triple every Azure DevOps API call
// addresses — resolved from the pipeline environment or a git remote.
//
// The credential is Provider.Token, supplied by the caller (see
// internal/cli wiring); it never appears in any error, log, plan, or
// the process table, and remote-URL errors never echo a URL's userinfo
// section, where Azure DevOps personal access tokens are commonly
// embedded.
package azuredevops

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

// Repo identifies an Azure Repos git repository: unlike GitHub's
// owner/repo pair, Azure DevOps addresses a repository through three
// coordinates.
type Repo struct {
	// Organization is the Azure DevOps organization (dev.azure.com/{org}).
	Organization string
	// Project is the team project the repository lives in.
	Project string
	// Name is the repository name within the project.
	Name string
}

// String renders the identity as organization/project/name for messages.
func (r Repo) String() string {
	return r.Organization + "/" + r.Project + "/" + r.Name
}

// ResolveRepo determines the Azure Repos repository identity: from the
// Azure Pipelines environment when this build checks out an Azure Repos
// repository (BUILD_REPOSITORY_PROVIDER=TfsGit), otherwise by parsing
// the origin remote URL.
func ResolveRepo(ctx context.Context) (Repo, error) {
	if r, ok := repoFromEnv(); ok {
		return r, nil
	}
	out, err := exec.CommandContext(ctx, "git", "remote", "get-url", "origin").Output()
	if err != nil {
		return Repo{}, fmt.Errorf("resolve repository from origin remote: %w", err)
	}
	return ParseRemoteURL(strings.TrimSpace(string(out)))
}

// repoFromEnv reads the identity the Azure Pipelines agent publishes for
// an Azure Repos checkout. The provider gate matters: for other
// providers (a GitHub-hosted repository built on Azure Pipelines)
// BUILD_REPOSITORY_NAME holds a different shape and must not be misread.
func repoFromEnv() (Repo, bool) {
	if !strings.EqualFold(os.Getenv("BUILD_REPOSITORY_PROVIDER"), "TfsGit") {
		return Repo{}, false
	}
	r := Repo{
		Organization: orgFromCollectionURI(os.Getenv("SYSTEM_TEAMFOUNDATIONCOLLECTIONURI")),
		Project:      os.Getenv("SYSTEM_TEAMPROJECT"),
		Name:         os.Getenv("BUILD_REPOSITORY_NAME"),
	}
	if r.Organization == "" || r.Project == "" || r.Name == "" {
		return Repo{}, false
	}
	return r, true
}

// orgFromCollectionURI extracts the organization from the collection URI
// the agent sets: https://dev.azure.com/{org}/ or the legacy
// https://{org}.visualstudio.com/.
func orgFromCollectionURI(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "dev.azure.com" {
		if segs := pathSegments(u.Path); len(segs) > 0 {
			return segs[0]
		}
		return ""
	}
	if org, ok := strings.CutSuffix(host, ".visualstudio.com"); ok {
		return org
	}
	return ""
}

// ParseRemoteURL extracts the repository identity from an Azure DevOps
// git remote URL in the shapes git actually configures:
//
//	https://dev.azure.com/{org}/{project}/_git/{repo}
//	https://{user}@dev.azure.com/{org}/{project}/_git/{repo}
//	git@ssh.dev.azure.com:v3/{org}/{project}/{repo}
//	ssh://git@ssh.dev.azure.com/v3/{org}/{project}/{repo}
//	https://{org}.visualstudio.com/[DefaultCollection/]{project}/_git/{repo}
//	{org}@vs-ssh.visualstudio.com:v3/{org}/{project}/{repo}
//
// Percent-encoded path segments (project names may contain spaces) are
// decoded. Any other URL is an error; the error never includes the
// URL's userinfo section, where a credential may be embedded.
func ParseRemoteURL(remote string) (Repo, error) {
	host, path, err := splitRemote(remote)
	if err != nil {
		return Repo{}, err
	}
	segs := pathSegments(path)
	switch {
	case host == "dev.azure.com":
		// {org}/{project}/_git/{repo}
		if len(segs) == 4 && segs[2] == "_git" {
			return Repo{Organization: segs[0], Project: segs[1], Name: segs[3]}, nil
		}
	case host == "ssh.dev.azure.com" || host == "vs-ssh.visualstudio.com":
		// v3/{org}/{project}/{repo}
		if len(segs) == 4 && segs[0] == "v3" {
			return Repo{Organization: segs[1], Project: segs[2], Name: segs[3]}, nil
		}
	case strings.HasSuffix(host, ".visualstudio.com"):
		org := strings.TrimSuffix(host, ".visualstudio.com")
		// {project}/_git/{repo}, optionally behind DefaultCollection/.
		if len(segs) == 3 && segs[1] == "_git" {
			return Repo{Organization: org, Project: segs[0], Name: segs[2]}, nil
		}
		if len(segs) == 4 && segs[0] == "DefaultCollection" && segs[2] == "_git" {
			return Repo{Organization: org, Project: segs[1], Name: segs[3]}, nil
		}
	}
	return Repo{}, fmt.Errorf("cannot parse an Azure DevOps repository from remote %q", host+"/"+path)
}

// splitRemote separates a git remote into host and path, handling URL
// (scheme://) and scp-like (user@host:path) forms. Userinfo is dropped,
// never returned, so callers cannot echo an embedded credential.
func splitRemote(remote string) (host, path string, err error) {
	s := strings.TrimSuffix(strings.TrimSpace(remote), ".git")
	if strings.Contains(s, "://") {
		u, perr := url.Parse(s)
		if perr != nil {
			// Deliberately NOT wrapped: a net/url parse error echoes the full
			// input, which may carry a credential in its userinfo section.
			return "", "", fmt.Errorf("cannot parse remote URL")
		}
		return u.Hostname(), strings.Trim(u.Path, "/"), nil
	}
	// scp-like: [user@]host:path
	if at := strings.LastIndex(s, "@"); at >= 0 {
		s = s[at+1:]
	}
	host, path, ok := strings.Cut(s, ":")
	if !ok || host == "" {
		return "", "", fmt.Errorf("cannot parse remote %q", s)
	}
	return host, strings.Trim(path, "/"), nil
}

// pathSegments splits a URL path into its non-empty segments, decoding
// percent-escapes (project names may contain spaces, encoded as %20). A
// segment that fails to decode is kept verbatim.
func pathSegments(path string) []string {
	var segs []string
	for _, s := range strings.Split(strings.Trim(path, "/"), "/") {
		if s == "" {
			continue
		}
		if dec, err := url.PathUnescape(s); err == nil {
			s = dec
		}
		segs = append(segs, s)
	}
	return segs
}
