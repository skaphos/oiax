package delivery

import (
	"encoding/json"

	"github.com/skaphos/oiax/v2/internal/notification"
	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

func encode(kind v1.NotificationTransport, p notification.DeliveryPayloadV1) ([]byte, error) {
	if p.SchemaVersion != 1 {
		return nil, notification.ErrInvalidState
	}
	facts, err := notification.FixedFacts(p.Event)
	if err != nil {
		return nil, err
	}
	var value any
	switch kind {
	case v1.NotificationWebhook:
		value = webhook(p, facts)
	case v1.NotificationTeams:
		value = teams(p, facts)
	case v1.NotificationSlack:
		value = slack(p, facts)
	default:
		return nil, notification.ErrInvalidState
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, notification.ErrInvalidState
	}
	if len(data) > 24<<10 {
		return nil, notification.ErrCapacity
	}
	return data, nil
}

func webhook(p notification.DeliveryPayloadV1, facts string) any {
	e := p.Event
	s := e.Snapshot
	commits := s.Commits
	if commits == nil {
		commits = []notification.CommitSummary{}
	}
	return struct {
		SchemaVersion      int                             `json:"schemaVersion"`
		ID                 string                          `json:"id"`
		Kind               v1.NotificationEvent            `json:"kind"`
		Repository         notification.RepositoryIdentity `json:"repository"`
		Graph              string                          `json:"graph"`
		Request            notification.RequestV1          `json:"request"`
		Message            notification.RenderedMessageV1  `json:"message"`
		Commits            []notification.CommitSummary    `json:"commits"`
		CommitCount        int                             `json:"commitCount"`
		CommitCountKnown   bool                            `json:"commitCountKnown"`
		CommitsTruncated   bool                            `json:"commitsTruncated"`
		CommitsUnavailable bool                            `json:"commitsUnavailable"`
		OccurredAt         string                          `json:"occurredAt"`
		ObservedAt         string                          `json:"observedAt"`
		Facts              string                          `json:"facts"`
	}{1, e.ID, e.Kind, e.Repository, e.Graph, e.Request, p.Message, commits, s.CommitCount, s.CommitCountKnown, s.CommitsTruncated, s.CommitsUnavailable, e.OccurredAt.UTC().Format("2006-01-02T15:04:05Z07:00"), e.ObservedAt.UTC().Format("2006-01-02T15:04:05Z07:00"), facts}
}
