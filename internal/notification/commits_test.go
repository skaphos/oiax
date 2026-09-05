package notification

import (
	"fmt"
	"strings"
	"testing"
)

func TestBoundSnapshot(t *testing.T) {
	base := CommitSnapshot{SourceOID: strings.Repeat("a", 40), BaseOID: strings.Repeat("b", 40), CommitCount: 101, CommitCountKnown: true}
	for i := range 101 {
		base.Commits = append(base.Commits, CommitSummary{SHA: fmt.Sprintf("%040x", i+1), Subject: strings.Repeat("é", 201) + "\nprivate body", URL: "https://untrusted.invalid/secret"})
	}
	got := BoundSnapshot(base)
	if got.CommitsUnavailable || !got.CommitsTruncated || len(got.Commits) != 100 || len([]rune(got.Commits[0].Subject)) != 200 || got.Commits[0].URL != "" {
		t.Fatalf("bad bounded snapshot: %+v", got)
	}
	if base.Commits[0].URL == "" || len(base.Commits) != 101 {
		t.Fatal("input mutated")
	}
	for _, mutate := range []func(*CommitSnapshot){func(s *CommitSnapshot) { s.SourceOID = "branch" }, func(s *CommitSnapshot) { s.CommitCount = 1 }, func(s *CommitSnapshot) { s.Commits[1].SHA = s.Commits[0].SHA }} {
		s := base
		s.Commits = append([]CommitSummary(nil), base.Commits...)
		mutate(&s)
		if !BoundSnapshot(s).CommitsUnavailable {
			t.Fatal("unverified snapshot accepted")
		}
	}
}
