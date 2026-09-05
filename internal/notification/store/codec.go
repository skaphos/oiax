// Package store persists bounded notification snapshots as ordinary Git notes.
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/skaphos/oiax/internal/notification"
	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

// Decode bounds reads before parsing, rejects duplicate keys (including escaped
// spellings), unknown schemas/fields and invalid cross-references. Parser errors
// are never returned verbatim because a rejected document may contain secrets.
func Decode(reader io.Reader) (*notification.LedgerV1, error) {
	data, err := io.ReadAll(io.LimitReader(reader, notification.MaxLedgerBytes+1))
	if err != nil {
		return nil, notification.ErrUnavailable
	}
	if len(data) > notification.MaxLedgerBytes {
		return nil, notification.ErrCapacity
	}
	if !utf8.Valid(data) || !uniqueJSON(data) {
		return nil, notification.ErrInvalidState
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var ledger notification.LedgerV1
	if err := dec.Decode(&ledger); err != nil {
		return nil, notification.ErrInvalidState
	}
	if err := Validate(&ledger); err != nil {
		return nil, err
	}
	return &ledger, nil
}

// Encode uses encoding/json's lexicographic map-key ordering for canonical bytes.
func Encode(l *notification.LedgerV1) ([]byte, error) {
	if err := Validate(l); err != nil {
		return nil, err
	}
	data, err := json.Marshal(l)
	if err != nil {
		return nil, notification.ErrInvalidState
	}
	if len(data) > notification.MaxLedgerBytes {
		return nil, notification.ErrCapacity
	}
	return data, nil
}

func uniqueJSON(data []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 64 {
			return false
		}
		token, err := dec.Token()
		if err != nil {
			return false
		}
		delim, isDelim := token.(json.Delim)
		if !isDelim {
			return true
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for dec.More() {
				token, err := dec.Token()
				if err != nil {
					return false
				}
				key, ok := token.(string)
				if !ok || keys[key] {
					return false
				}
				keys[key] = true
				if !value(depth + 1) {
					return false
				}
			}
		case '[':
			for dec.More() {
				if !value(depth + 1) {
					return false
				}
			}
		default:
			return false
		}
		closing, err := dec.Token()
		return err == nil && (delim == '{' && closing == json.Delim('}') || delim == '[' && closing == json.Delim(']'))
	}
	if !value(0) {
		return false
	}
	_, err := dec.Token()
	return errors.Is(err, io.EOF)
}

func safeText(s string, max int) bool {
	return utf8.ValidString(s) && len(s) <= max && !strings.ContainsFunc(s, unicode.IsControl)
}
func eventKind(e v1.NotificationEvent) bool {
	return e == v1.NotificationRequestCreated || e == v1.NotificationRequestMerged
}
func requestKind(k v1.NotificationRequestType) bool {
	return k == v1.NotificationPromotion || k == v1.NotificationBackflow
}

func validRepository(r notification.RepositoryIdentity) bool {
	if r.ID == "" || r.Host == "" || !safeText(r.ID, 256) || !safeText(r.Name, 1024) || !safeText(r.Host, 256) {
		return false
	}
	switch r.Provider {
	case "github":
		return r.OrganizationID == "" && r.ProjectID == ""
	case "azuredevops":
		return r.OrganizationID != "" && r.ProjectID != "" && safeText(r.OrganizationID, 256) && safeText(r.ProjectID, 256)
	default:
		return false
	}
}

func validRequest(r notification.RequestV1) bool {
	return r.ID != "" && r.Source != "" && r.Destination != "" && requestKind(r.Type) && safeText(r.ID, 256) && safeText(r.Source, 1024) && safeText(r.Destination, 1024) && safeText(r.LogicalSource, 1024) && safeText(r.LogicalDestination, 1024) && safeText(r.URL, 4096)
}

func validEvent(e notification.EventV1, l *notification.LedgerV1) bool {
	if !eventKind(e.Kind) || !validRepository(e.Repository) || !e.Repository.Same(l.Repository) || e.Graph != l.Graph || !validRequest(e.Request) || e.ID != notification.EventID(e.Repository, e.Request.ID, e.Kind) || e.OccurredAt.IsZero() || e.ObservedAt.IsZero() || !safeText(e.SourceEnvironment, 1024) || !safeText(e.DestinationEnvironment, 1024) {
		return false
	}
	s := e.Snapshot
	if len(s.Commits) > notification.MaxCommits || s.CommitCount < 0 || (!s.CommitCountKnown && s.CommitCount != 0) || (s.CommitCountKnown && s.CommitCount < len(s.Commits)) || (s.CommitsUnavailable && len(s.Commits) != 0) {
		return false
	}
	for _, oid := range []string{s.SourceOID, s.BaseOID, s.MergeResultOID} {
		if oid != "" && !notification.ValidOID(oid) {
			return false
		}
	}
	for _, c := range s.Commits {
		if !notification.ValidOID(c.SHA) || len(c.ShortSHA) < 7 || !strings.HasPrefix(c.SHA, c.ShortSHA) || !safeText(c.Subject, 800) || utf8.RuneCountInString(c.Subject) > 200 || !safeText(c.URL, 4096) {
			return false
		}
	}
	return true
}

// Validate checks identity and all durable references without reaching Git or
// accepting metadata as request ownership evidence.
func Validate(l *notification.LedgerV1) error {
	bad := notification.ErrInvalidState
	if l == nil || l.Version != notification.SchemaVersion || !validRepository(l.Repository) || l.Graph == "" || !safeText(l.Graph, 1024) || !notification.ValidOID(l.AnchorOID) || !notification.ValidOID(l.PolicyRevision.ConfigOID) || !notification.ValidDigest(l.PolicyRevision.PolicyDigest) || l.Destinations == nil || l.Events == nil || l.Deliveries == nil || l.KnownRequests == nil || l.Scans == nil {
		return bad
	}
	if len(l.Deliveries) > notification.MaxDeliveries {
		return notification.ErrCapacity
	}
	for name, d := range l.Destinations {
		if name != d.Name || name == "" || len(name) > 63 || !safeText(name, 63) || !notification.ValidDigest(d.Fingerprint) || !notification.ValidDigest(d.Generation) || d.ActivatedAt.IsZero() || d.Subscriptions == nil || len(d.Lease.AttemptID) > 128 {
			return bad
		}
		for key, sub := range d.Subscriptions {
			if !eventKind(sub.Event) || !requestKind(sub.RequestType) || key != notification.SubscriptionKey(sub.Event, sub.RequestType) || sub.Cutoff.IsZero() {
				return bad
			}
		}
	}
	for id, e := range l.Events {
		if id != e.ID || !validEvent(e, l) {
			return bad
		}
	}
	for key, r := range l.Deliveries {
		_, eventExists := l.Events[r.EventID]
		_, destinationExists := l.Destinations[r.Destination]
		if !eventExists || !destinationExists || !notification.ValidDigest(r.Generation) || key != notification.DeliveryKey(r.EventID, r.Destination, r.Generation) || r.Attempts < 0 || len(r.AttemptIDs) != r.Attempts || len(r.Lease.AttemptID) > 128 {
			return bad
		}
		seen := map[string]bool{}
		for _, id := range r.AttemptIDs {
			if id == "" || !safeText(id, 128) || seen[id] {
				return bad
			}
			seen[id] = true
		}
		if r.Lease.AttemptID != "" && !seen[r.Lease.AttemptID] {
			return bad
		}
		if r.Code != "" && !notification.ValidOutcome(r.Code) {
			return bad
		}
		switch r.Status {
		case notification.StatusPending:
		case notification.StatusClaimed:
			if r.Lease.AttemptID == "" || r.Lease.Until.IsZero() || r.Message == nil {
				return bad
			}
		case notification.StatusRetryable:
			if r.NextAttemptAt.IsZero() || r.Attempts == 0 {
				return bad
			}
		case notification.StatusDelivered:
			if r.DeliveredAt.IsZero() || r.AcceptedAt.IsZero() || r.Code != notification.OutcomeAccepted || r.Attempts == 0 || r.Message == nil {
				return bad
			}
		case notification.StatusSkipped:
			if r.Code != notification.OutcomeRetired {
				return bad
			}
		default:
			return bad
		}
		if r.Message != nil && (!safeText(r.Message.Title, 1024) || utf8.RuneCountInString(r.Message.Title) > 256 || !utf8.ValidString(r.Message.Body) || len(r.Message.Body) > 12<<10) {
			return bad
		}
	}
	for id, r := range l.KnownRequests {
		if id != r.Request.ID || !validRequest(r.Request) || !r.Repository.Same(l.Repository) || r.Graph != l.Graph || r.CreatedAt.IsZero() {
			return bad
		}
		switch r.State {
		case notification.LifecycleOpen, notification.LifecycleClosed:
		case notification.LifecycleMerged:
			if r.MergedAt.IsZero() {
				return bad
			}
		default:
			return bad
		}
		if r.Origin != nil {
			o := r.Origin
			if !notification.ValidOrigin(*o) || o.Graph != l.Graph {
				return bad
			}
		}
	}
	for key, s := range l.Scans {
		if !safeText(key, 128) || s.Version != 1 || !safeText(s.Cursor, 16384) || s.Through.Before(s.From) {
			return bad
		}
	}
	return nil
}
