package reconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skaphos/oiax/v2/internal/engine"
	"github.com/skaphos/oiax/v2/internal/forge"
	"github.com/skaphos/oiax/v2/internal/git"
	"github.com/skaphos/oiax/v2/internal/gittest"
	"github.com/skaphos/oiax/v2/internal/notification"
	notificationstore "github.com/skaphos/oiax/v2/internal/notification/store"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

type notificationRuntimeForge struct {
	*fakeForge
	remote   string
	identity notification.RepositoryIdentity
}

func (f *notificationRuntimeForge) RepositoryIdentity(context.Context) (notification.RepositoryIdentity, error) {
	return f.identity, nil
}

func (f *notificationRuntimeForge) ListLifecyclePage(_ context.Context, query forge.LifecycleQuery) (forge.LifecyclePage, error) {
	return forge.LifecyclePage{Pages: 1, Progress: notification.ScanProgress{Version: 1, From: query.From, Through: query.Through, Complete: true}}, nil
}

func (f *notificationRuntimeForge) GetLifecycleRequest(context.Context, forge.RequestID) (notification.LifecycleRequest, error) {
	return notification.LifecycleRequest{}, notification.ErrRequestMissing
}

func (f *notificationRuntimeForge) OpenNotificationNotes(ctx context.Context, graphKey string) (*git.NotificationNotes, error) {
	return git.OpenNotificationNotes(ctx, git.NotesOptions{Remote: f.remote, GraphKey: graphKey})
}

func TestFinalizeNotificationsInitializesOnlyNotesState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	work := t.TempDir()
	gittest.InitRepo(t, work)
	if err := os.WriteFile(filepath.Join(work, "config"), []byte("pinned"), 0o600); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, work, "add", "config")
	gittest.Run(t, work, "commit", "-qm", "config")
	configOID := gittest.Run(t, work, "rev-parse", "HEAD")
	remote := t.TempDir()
	gittest.Run(t, remote, "init", "--bare", "-q")
	gittest.Run(t, work, "remote", "add", "origin", remote)
	gittest.Run(t, work, "push", "-q", "origin", "main")
	repository := notification.RepositoryIdentity{Provider: "github", Host: "github.com", ID: "1", Name: "example/repo"}
	runtimeForge := &notificationRuntimeForge{fakeForge: &fakeForge{}, remote: remote, identity: repository}
	coordinator := &Coordinator{
		Git:                &git.Runner{Dir: work},
		Forge:              runtimeForge,
		Graph:              &engine.Graph{Name: "graph"},
		ConfigOID:          configOID,
		NotificationPolicy: &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "NEVER_READ"}}},
	}
	if err := coordinator.FinalizeNotifications(ctx); err != nil {
		t.Fatal(err)
	}
	refs := gittest.Run(t, remote, "show-ref")
	if !strings.Contains(refs, "refs/notes/oiax/notifications/v1/") || strings.Contains(refs, "refs/heads/oiax/") || strings.Contains(refs, "refs/tags/") {
		t.Fatalf("unexpected notification refs: %s", refs)
	}
	notes, err := git.OpenNotificationNotes(ctx, git.NotesOptions{Remote: remote, GraphKey: notification.GraphKey(repository, "graph")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := notes.Close(); err != nil {
			t.Error(err)
		}
	}()
	state, err := notificationstore.New(notes, repository, "graph").Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Ledger.PolicyRevision.ConfigOID != configOID || !state.Ledger.Destinations["ops"].Active {
		t.Fatalf("notification policy not durably initialized: %+v", state.Ledger.PolicyRevision)
	}
}

func TestFinalizeNotificationsDisabledAndCanceledBypassCapabilities(t *testing.T) {
	t.Parallel()
	coordinator := &Coordinator{NotificationPolicy: &v1.NotificationPolicy{}}
	if err := coordinator.FinalizeNotifications(context.Background()); err != nil {
		t.Fatal(err)
	}
	coordinator.NotificationPolicy = &v1.NotificationPolicy{Destinations: []v1.NotificationDestination{{Name: "ops", Type: v1.NotificationWebhook, EndpointEnv: "ENDPOINT"}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	coordinator.Git = &git.Runner{}
	coordinator.Graph = &engine.Graph{Name: "graph"}
	coordinator.ConfigOID = strings.Repeat("a", 40)
	if err := coordinator.FinalizeNotifications(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled finalization = %v", err)
	}
}
