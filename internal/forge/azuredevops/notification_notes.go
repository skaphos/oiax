package azuredevops

import (
	"context"

	"github.com/skaphos/oiax/v2/internal/forge"
	"github.com/skaphos/oiax/v2/internal/git"
)

func (p *Provider) OpenNotificationNotes(ctx context.Context, graphKey string) (*git.NotificationNotes, error) {
	return git.OpenNotificationNotes(ctx, git.NotesOptions{Remote: p.gitRemote(), GraphKey: graphKey, Env: p.pushAuthEnv()})
}

var _ forge.NotificationNotesProvider = (*Provider)(nil)
