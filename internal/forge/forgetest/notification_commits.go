package forgetest

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/skaphos/oiax/v2/internal/forge"
	"github.com/skaphos/oiax/v2/internal/notification"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

// SnapshotCase identifies fixture-owned historical evidence. A fixture must not
// resolve a moving branch to satisfy these checks.
//
// Mode "tampered-origin" is the adversarial case: the fixture rewrites the
// origin block in the request's own editable text so it claims the
// TamperedBaseOID..TamperedSourceOID range, and serves that range as real
// history with TamperedCount commits. A provider that took evidence from
// request text lists it and fails; a provider may instead decline to attest
// membership at all.
type SnapshotCase struct {
	Kind  v1.NotificationEvent
	Mode  string
	Count int
}

// Evidence a tampered origin block claims. The OIDs are deliberately unrelated
// to the fixture's real head and base so any leak into a snapshot is visible.
const (
	TamperedSourceOID    = "ffffffffffffffffffffffffffffffffffffffff"
	TamperedBaseOID      = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	TamperedCommitPrefix = "ff"
	TamperedCount        = 99
)

// TamperedCommitOID is the i-th commit of the range a tampered block claims.
func TamperedCommitOID(i int) string {
	return TamperedCommitPrefix + fmt.Sprintf("%038x", i+1)
}

func RunNotificationSnapshots(t *testing.T, factory func(*testing.T, SnapshotCase) (forge.SnapshotReader, notification.LifecycleRequest)) {
	t.Helper()
	for _, kind := range []v1.NotificationEvent{v1.NotificationRequestCreated, v1.NotificationRequestMerged} {
		for _, mode := range []string{"source-advanced", "squash", "rebase", "deleted-ref", "unavailable", "truncated", "tampered-origin"} {
			t.Run(string(kind)+"/"+mode, func(t *testing.T) {
				count := 2
				if mode == "truncated" {
					count = 101
				}
				reader, req := factory(t, SnapshotCase{Kind: kind, Mode: mode, Count: count})
				got, err := reader.GetCommitSnapshot(context.Background(), req, forge.EventRevision{Kind: kind, SourceOID: req.SourceOID, BaseOID: req.BaseOID, MergeResultOID: req.MergeResultOID})
				if mode == "unavailable" {
					if !got.CommitsUnavailable || len(got.Commits) != 0 {
						t.Fatal("unverified history asserted", err)
					}
					return
				}
				if mode == "tampered-origin" {
					if got.SourceOID == TamperedSourceOID || got.BaseOID == TamperedBaseOID || (got.CommitCountKnown && got.CommitCount == TamperedCount) {
						t.Fatalf("editable request text became commit evidence: %+v", got)
					}
					for _, c := range got.Commits {
						if strings.HasPrefix(c.SHA, TamperedCommitPrefix) {
							t.Fatalf("tampered range listed as membership: %+v", got)
						}
					}
					// Declining to attest membership is a sound outcome; asserting
					// any membership at all must still satisfy every check below.
					if got.CommitsUnavailable {
						return
					}
				}
				if err != nil || got.CommitsUnavailable || len(got.Commits) != min(count, notification.MaxCommits) {
					t.Fatalf("snapshot length %d: %v", len(got.Commits), err)
				}
				if got.SourceOID != strings.Repeat("a", 40) || got.MergeResultOID == got.SourceOID {
					t.Fatal("moving head or merge result substituted for reviewed source")
				}
				if mode == "truncated" && !got.CommitsTruncated {
					t.Fatal("truncation not reported")
				}
				if !got.CommitCountKnown && got.CommitCount != 0 {
					t.Fatal("fabricated total")
				}
				for _, c := range got.Commits {
					if !notification.ValidOID(c.SHA) || len([]rune(c.Subject)) > 200 || strings.ContainsAny(c.Subject, "\n\x1b\u202e") {
						t.Fatal("unsafe summary")
					}
				}
			})
		}
	}
}
