package notification

import "strings"

// BoundSnapshot validates and copies enrichment before admission. Provider URLs
// are discarded: the independently verified PR link remains the review record.
func BoundSnapshot(s CommitSnapshot) CommitSnapshot {
	unavailable := CommitSnapshot{CommitsUnavailable: true}
	if s.CommitsUnavailable || !ValidOID(s.SourceOID) || !ValidOID(s.BaseOID) || (s.MergeResultOID != "" && !ValidOID(s.MergeResultOID)) || s.CommitCount < 0 {
		return unavailable
	}
	if s.CommitCountKnown && s.CommitCount < len(s.Commits) {
		return unavailable
	}
	if !s.CommitCountKnown {
		s.CommitCount = 0
	}
	if len(s.Commits) > MaxCommits {
		s.CommitsTruncated = true
	}
	commits := make([]CommitSummary, 0, min(len(s.Commits), MaxCommits))
	seen := map[string]bool{}
	for _, c := range s.Commits[:min(len(s.Commits), MaxCommits)] {
		if !ValidOID(c.SHA) || seen[c.SHA] {
			return unavailable
		}
		seen[c.SHA] = true
		subject, _, _ := strings.Cut(c.Subject, "\n")
		runes := []rune(CleanText(subject, false))
		if len(runes) > 200 {
			runes = append(runes[:199], '…')
			s.CommitsTruncated = true
		}
		commits = append(commits, CommitSummary{SHA: c.SHA, ShortSHA: c.SHA[:7], Subject: string(runes)})
	}
	s.Commits = commits
	if s.CommitCountKnown && s.CommitCount > len(commits) {
		s.CommitsTruncated = true
	}
	return s
}
