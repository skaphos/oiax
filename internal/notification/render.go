package notification

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

// CleanText removes terminal controls and bidi format controls. Newlines remain
// useful in bodies; adapters encode all user text as inert content, never markup.
func CleanText(s string, multiline bool) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' && multiline {
			return r
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, "�"))
}

func SafeRequestURL(e EventV1) bool {
	u, err := url.Parse(e.Request.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Hostname() == "" || !strings.EqualFold(u.Hostname(), e.Repository.Host) || u.Port() != "" {
		return false
	}
	if e.Request.ID == "" {
		return false
	}
	suffix := "/pull/" + url.PathEscape(e.Request.ID)
	if e.Repository.Provider == "azuredevops" {
		suffix = "/pullrequest/" + url.PathEscape(e.Request.ID)
	}
	return strings.HasSuffix(u.EscapedPath(), suffix)
}

// FixedFacts is independent of template content and transport. Required identity
// is never silently truncated to fit; callers retain a pending size diagnostic.
func FixedFacts(e EventV1) (string, error) {
	if e.ID != EventID(e.Repository, e.Request.ID, e.Kind) || e.Graph == "" || e.Repository.ID == "" || e.Repository.Name == "" || e.Request.Source == "" || e.Request.Destination == "" || e.ObservedAt.IsZero() || !SafeRequestURL(e) {
		return "", ErrInvalidState
	}
	if (e.Kind != v1.NotificationRequestCreated && e.Kind != v1.NotificationRequestMerged) || (e.Request.Type != v1.NotificationPromotion && e.Request.Type != v1.NotificationBackflow) {
		return "", ErrInvalidState
	}
	fields := []string{e.Repository.Name, e.Graph, e.Request.Source, e.Request.Destination, e.Request.ID, e.Request.URL}
	for _, s := range fields {
		if !utf8.ValidString(s) || CleanText(s, false) != s {
			return "", ErrInvalidState
		}
	}
	facts := fmt.Sprintf("Repository: %s (%s/%s/%s)\nGraph: %s\nEvent: %s\nRequest type: %s\nSource: %s\nDestination: %s\nEvent ID: %s\nRequest ID: %s\nRequest: %s\nObserved at: %s", e.Repository.Name, e.Repository.Provider, e.Repository.Host, e.Repository.ID, e.Graph, e.Kind, e.Request.Type, e.Request.Source, e.Request.Destination, e.ID, e.Request.ID, e.Request.URL, e.ObservedAt.UTC().Format(time.RFC3339))
	if e.Snapshot.CommitsUnavailable {
		facts += "\nCommit details unavailable; see the request."
	} else if e.Snapshot.CommitsTruncated {
		facts += "\nCommit details truncated; see the request for the full review."
	} else if !e.Snapshot.CommitCountKnown {
		facts += "\nCommit total unknown; see the request."
	} else {
		facts += fmt.Sprintf("\nCommit count: %d", e.Snapshot.CommitCount)
	}
	if len(facts) > 12<<10 {
		return "", ErrCapacity
	}
	return facts, nil
}

// RenderBuiltin describes branch promotion/backflow, never deployment status.
// The complete identity section is appended independently by every adapter.
func RenderBuiltin(e EventV1) (RenderedMessageV1, error) {
	if _, err := FixedFacts(e); err != nil {
		return RenderedMessageV1{}, err
	}
	destination := e.DestinationEnvironment
	if destination == "" {
		destination = e.Request.Destination
	}
	var title, body string
	if e.Kind == v1.NotificationRequestCreated {
		title = "Branch promotion ready for review"
		body = "These commits are ready for review for the " + destination + " environment."
		if e.Request.Type == v1.NotificationBackflow {
			title = "Backflow ready for review"
			body = "These commits are ready for review to return to " + destination + " by backflow."
		}
	} else {
		title = "Branch promotion completed"
		body = "These commits were promoted to the " + destination + " environment."
		if e.Request.Type == v1.NotificationBackflow {
			title = "Backflow completed"
			body = "These commits were returned to " + destination + " by backflow."
		}
	}
	if e.Snapshot.CommitsUnavailable {
		body += "\n\nCommit details unavailable; see the request."
	}
	return RenderedMessageV1{Title: CleanText(title, false), Body: CleanText(body, true)}, nil
}
