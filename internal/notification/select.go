package notification

import (
	"time"

	"github.com/skaphos/oiax/internal/engine"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

// MergeEvent normalizes provider-verified facts. It never guesses a merge from
// branch equality or a legacy backflow's logical source from its branch name.
func MergeEvent(graph *engine.Graph, policy *v1.NotificationPolicy, req LifecycleRequest, observedAt time.Time) (EventV1, bool) {
	if graph == nil || req.Graph != graph.Name || req.State != LifecycleMerged || req.MergedAt.IsZero() || req.Request.ID == "" {
		return EventV1{}, false
	}
	r := req.Request
	eligible := false
	switch r.Type {
	case v1.NotificationPromotion:
		for _, edge := range graph.Promotions {
			if edge.From == r.Source && edge.To == r.Destination {
				eligible = true
				break
			}
		}
	case v1.NotificationBackflow:
		if graph.Backflow.Enabled && r.Destination == graph.Backflow.Target {
			if r.LogicalSource == "" {
				eligible = true
			} else {
				for _, source := range graph.Backflow.Sources {
					if source == r.LogicalSource {
						eligible = true
						break
					}
				}
			}
		}
	}
	if !eligible {
		return EventV1{}, false
	}
	label := func(logical, actual string) string {
		branch := logical
		if branch == "" {
			branch = actual
		}
		if policy != nil && policy.EnvironmentNames[branch] != "" {
			return policy.EnvironmentNames[branch]
		}
		return branch
	}
	e := EventV1{ID: EventID(req.Repository, r.ID, v1.NotificationRequestMerged), Kind: v1.NotificationRequestMerged, Repository: req.Repository, Graph: req.Graph, Request: r, OccurredAt: req.MergedAt.UTC(), ObservedAt: observedAt.UTC(), SourceEnvironment: label(r.LogicalSource, r.Source), DestinationEnvironment: label(r.LogicalDestination, r.Destination), Snapshot: CommitSnapshot{CommitsUnavailable: true}}
	return e, true
}
