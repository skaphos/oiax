package notification

import (
	"fmt"
	"testing"
	"time"
)

func TestDueDeliveriesBoundsAndFairness(t *testing.T) {
	t.Parallel()
	now := modelTime()
	ledger := NewLedger(modelRepo(), "graph", modelRevision("a").ConfigOID)
	ledger.PolicyRevision = modelRevision("a")
	for destination := range 20 {
		name := fmt.Sprintf("destination-%02d", destination)
		generation := Digest("generation", name)
		ledger.Destinations[name] = DestinationState{Name: name, Generation: generation, Active: true}
		for eventIndex := range 12 {
			event := modelEvent()
			event.Request.ID = fmt.Sprintf("%02d-%02d", destination, eventIndex)
			event.ID = EventID(event.Repository, event.Request.ID, event.Kind)
			event.OccurredAt = now.Add(time.Duration(eventIndex) * time.Second)
			ledger.Events[event.ID] = event
			key := DeliveryKey(event.ID, name, generation)
			ledger.Deliveries[key] = DeliveryRecord{EventID: event.ID, Destination: name, Generation: generation, Status: StatusPending}
		}
	}
	due := DueDeliveries(ledger, now.Add(time.Hour))
	if len(due) != 100 {
		t.Fatalf("global bound = %d", len(due))
	}
	counts := map[string]int{}
	for _, key := range due {
		counts[ledger.Deliveries[key].Destination]++
	}
	for name, count := range counts {
		if count != 5 {
			t.Fatalf("round-robin allocation for %s = %d", name, count)
		}
	}
}

func TestDueDeliveriesEligibilityFences(t *testing.T) {
	t.Parallel()
	now := modelTime()
	ledger := modelLedger(t)
	event := modelEvent()
	var err error
	ledger, err = AdmitEvent(ledger, ledger.PolicyRevision.ConfigOID, event)
	if err != nil {
		t.Fatal(err)
	}
	destination := ledger.Destinations["ops"]
	key := DeliveryKey(event.ID, destination.Name, destination.Generation)
	if got := DueDeliveries(ledger, now); len(got) != 1 || got[0] != key {
		t.Fatalf("initial due = %v", got)
	}

	for _, mutate := range []func(*LedgerV1){
		func(l *LedgerV1) {
			record := l.Deliveries[key]
			record.NextAttemptAt = now.Add(time.Nanosecond)
			l.Deliveries[key] = record
		},
		func(l *LedgerV1) {
			record := l.Deliveries[key]
			record.Lease.Until = now.Add(time.Minute)
			l.Deliveries[key] = record
		},
		func(l *LedgerV1) {
			state := l.Destinations["ops"]
			state.Lease.Until = now.Add(time.Minute)
			l.Destinations["ops"] = state
		},
		func(l *LedgerV1) {
			state := l.Destinations["ops"]
			state.NextSendAt = now.Add(time.Nanosecond)
			l.Destinations["ops"] = state
		},
		func(l *LedgerV1) { state := l.Destinations["ops"]; state.Active = false; l.Destinations["ops"] = state },
		func(l *LedgerV1) {
			record := l.Deliveries[key]
			record.Generation = "retired"
			l.Deliveries[key] = record
		},
		func(l *LedgerV1) {
			record := l.Deliveries[key]
			record.Status = StatusDelivered
			l.Deliveries[key] = record
		},
		func(l *LedgerV1) {
			record := l.Deliveries[key]
			record.Status = StatusSkipped
			l.Deliveries[key] = record
		},
	} {
		candidate := ledger.Clone()
		mutate(candidate)
		if got := DueDeliveries(candidate, now); len(got) != 0 {
			t.Fatalf("fenced delivery selected: %v", got)
		}
	}
}
