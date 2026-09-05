package notification

import (
	"sort"
	"time"
)

// DueDeliveries fairly allocates the bounded run budget in destination rounds.
// The caller still rechecks/claims each record against the latest durable state.
func DueDeliveries(l *LedgerV1, now time.Time) []string {
	groups := map[string][]string{}
	for key, r := range l.Deliveries {
		d := l.Destinations[r.Destination]
		if !d.Active || d.Generation != r.Generation || r.Status == StatusDelivered || r.Status == StatusSkipped || now.Before(r.NextAttemptAt) || now.Before(r.Lease.Until) || now.Before(d.Lease.Until) {
			continue
		}
		groups[r.Destination] = append(groups[r.Destination], key)
	}
	names := make([]string, 0, len(groups))
	for name, keys := range groups {
		names = append(names, name)
		sort.Slice(keys, func(i, j int) bool {
			a, b := l.Events[l.Deliveries[keys[i]].EventID], l.Events[l.Deliveries[keys[j]].EventID]
			if !a.OccurredAt.Equal(b.OccurredAt) {
				return a.OccurredAt.Before(b.OccurredAt)
			}
			return a.ID < b.ID
		})
	}
	sort.Strings(names)
	var result []string
	for round := 0; round < 10 && len(result) < 100; round++ {
		for _, name := range names {
			if round < len(groups[name]) {
				result = append(result, groups[name][round])
				if len(result) == 100 {
					break
				}
			}
		}
	}
	return result
}
