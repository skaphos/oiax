package forgetest

import (
	"context"
	"strings"
	"testing"

	"github.com/skaphos/oiax/v2/internal/forge"
	"github.com/skaphos/oiax/v2/internal/notification"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

// SnapshotCase identifies fixture-owned historical evidence. A fixture must not
// resolve a moving branch to satisfy these checks.
type SnapshotCase struct {
	Kind  v1.NotificationEvent
	Mode  string
	Count int
}

func RunNotificationSnapshots(t *testing.T, factory func(*testing.T, SnapshotCase) (forge.SnapshotReader, notification.LifecycleRequest)) {
	t.Helper()
	for _, kind := range []v1.NotificationEvent{v1.NotificationRequestCreated, v1.NotificationRequestMerged} {
		for _, mode := range []string{"source-advanced", "squash", "rebase", "deleted-ref", "unavailable", "truncated"} {
			t.Run(string(kind)+"/"+mode, func(t *testing.T) {
				count := 2
				if mode == "truncated" {
					count = 101
				}
				reader, req := factory(t, SnapshotCase{Kind: kind, Mode: mode, Count: count})
				got, err := reader.GetCommitSnapshot(context.Background(), req, forge.EventRevision{Kind: kind, SourceOID: req.SourceOID, BaseOID: req.BaseOID, MergeResultOID: req.MergeResultOID})
				if mode == "unavailable" || (req.Repository.Provider == "github" && kind == v1.NotificationRequestCreated) {
					if !got.CommitsUnavailable || len(got.Commits) != 0 {
						t.Fatal("unverified history asserted", err)
					}
					return
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
