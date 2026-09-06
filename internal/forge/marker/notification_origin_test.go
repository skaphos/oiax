package marker

import (
	"strings"
	"testing"
	"time"

	"github.com/skaphos/oiax/v2/internal/notification"
)

func testNotificationOrigin() notification.NotificationOriginV1 {
	return notification.NotificationOriginV1{Version: 1, OperationID: "create-operation", Graph: "graph", ConfigOID: strings.Repeat("a", 40), ObservedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), LogicalSource: "dev", LogicalTarget: "test", SourceOID: strings.Repeat("b", 40), BaseOID: strings.Repeat("c", 40)}
}

func TestNotificationOriginRoundTripAndMarkerPreservation(t *testing.T) {
	t.Parallel()
	o := testNotificationOrigin()
	o.OperationID = "opaque--><&<!--operation"
	m := Marker{Version: Version, Graph: "graph", Type: "promotion", Source: "dev", Destination: "test", SourceHead: o.SourceOID}
	legacy := "Human text\n\n" + Serialize(m) + "\nTrailing text"
	body, err := AppendNotificationOrigin(legacy, &o)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(body, "<!--") != 2 || !strings.Contains(body, `\u003e`) || !strings.Contains(body, `\u003c`) || !strings.Contains(body, `\u0026`) {
		t.Fatal("origin escaped its HTML comment")
	}
	got, ok := ParseNotificationOrigin(body)
	if !ok || got != o || !NotificationOriginMatches(got, m) {
		t.Fatalf("origin round trip = %+v, %v", got, ok)
	}
	if parsed, ok := Parse(body); !ok || parsed != m {
		t.Fatal("origin changed ownership parsing")
	}
	m.SourceHead = strings.Repeat("d", 40)
	updated, ok := Replace(body, m)
	if !ok {
		t.Fatal("marker replacement failed")
	}
	if got, ok := ParseNotificationOrigin(updated); !ok || got != o {
		t.Fatal("baseline update changed immutable origin")
	}
	if strings.TrimSuffix(updated, strings.TrimPrefix(body, legacy)) != "Human text\n\n"+Serialize(m)+"\nTrailing text" {
		t.Fatal("existing marker format or prose changed")
	}
	if noOrigin, err := AppendNotificationOrigin(legacy, nil); err != nil || noOrigin != legacy {
		t.Fatal("nil origin changed legacy body", err)
	}
	if noOrigin, err := AppendNotificationOrigin("Human text <!-- unfinished", nil); err != nil || noOrigin != "Human text <!-- unfinished" {
		t.Fatal("disabled provenance changed legacy free text", err)
	}
	for _, origin := range []*notification.NotificationOriginV1{nil, &o} {
		if _, err := AppendNotificationOrigin(body, origin); err == nil {
			t.Fatal("template-supplied origin accepted")
		}
	}
	only, err := AppendNotificationOrigin("", &o)
	if err != nil {
		t.Fatal(err)
	}
	if _, owned := Parse(only); owned {
		t.Fatal("origin granted ownership")
	}
}

func TestNotificationOriginRejectsAmbiguousAndMalformedBlocks(t *testing.T) {
	t.Parallel()
	o := testNotificationOrigin()
	valid, err := AppendNotificationOrigin("", &o)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"duplicate blocks":       valid + valid,
		"duplicate key":          strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1),
		"case alias":             strings.Replace(valid, `"version":1`, `"version":1,"Version":1`, 1),
		"unknown field":          strings.Replace(valid, `"version":1`, `"version":1,"extra":true`, 1),
		"unknown version":        strings.Replace(valid, `"version":1`, `"version":2`, 1),
		"bad OID":                strings.Replace(valid, o.ConfigOID, "not-an-oid", 1),
		"missing logical source": strings.Replace(valid, `"logicalSource":"dev"`, `"logicalSource":""`, 1),
		"control text":           strings.Replace(valid, "create-operation", `operation\n`, 1),
		"trailing JSON":          strings.Replace(valid, " -->", " {} -->", 1),
		"invalid second block":   valid + "<!-- oiax-notification-origin:not-json -->",
		"unclosed second block":  valid + "<!-- oiax-notification-origin:",
		"oversized":              strings.Replace(valid, "create-operation", strings.Repeat("x", 4096), 1),
		"comment terminator":     strings.Replace(valid, "create-operation", "-->forged", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := ParseNotificationOrigin(body); ok {
				t.Fatal("malformed origin accepted")
			}
		})
	}
	for _, field := range []string{"operation", "config", "source", "base", "time", "graph"} {
		bad := o
		switch field {
		case "operation":
			bad.OperationID = ""
		case "config":
			bad.ConfigOID = "bad"
		case "source":
			bad.SourceOID = "bad"
		case "base":
			bad.BaseOID = "bad"
		case "time":
			bad.ObservedAt = time.Time{}
		case "graph":
			bad.Graph = ""
		}
		if _, err := AppendNotificationOrigin("", &bad); err == nil {
			t.Fatalf("invalid %s encoded", field)
		}
	}
	o.Graph = strings.Repeat("<&", 500) // valid text, escaped block exceeds 4 KiB
	if _, err := AppendNotificationOrigin("", &o); err == nil {
		t.Fatal("escaped size bound not enforced")
	}
}

func TestNotificationOriginRequiresMatchingOwnership(t *testing.T) {
	t.Parallel()
	o := testNotificationOrigin()
	m := Marker{Version: "v1", Graph: "graph", Type: "promotion", Source: "dev", Destination: "test"}
	for _, mutate := range []func(*Marker){
		func(m *Marker) { m.Graph = "other" },
		func(m *Marker) { m.Source = "other" },
		func(m *Marker) { m.Destination = "other" },
		func(m *Marker) { m.Type = "conflict" },
		func(m *Marker) { m.Version = "bad" },
	} {
		bad := m
		mutate(&bad)
		if NotificationOriginMatches(o, bad) {
			t.Fatalf("origin accepted unrelated ownership: %+v", bad)
		}
	}
	m.Type, m.Source = "backflow", "oiax/backflow/deleted/abcdef0"
	if !NotificationOriginMatches(o, m) {
		t.Fatal("backflow confused actual and logical source")
	}
}

func FuzzNotificationOrigin(f *testing.F) {
	o := testNotificationOrigin()
	body, _ := AppendNotificationOrigin("", &o)
	f.Add(body)
	f.Add(body + body)
	f.Add("<!-- oiax-notification-origin:{} -->")
	f.Add("<!-->")
	f.Fuzz(func(t *testing.T, body string) {
		o, ok := ParseNotificationOrigin(body)
		if !ok {
			return
		}
		encoded, err := AppendNotificationOrigin("", &o)
		if err != nil {
			t.Fatal("accepted origin cannot be encoded", err)
		}
		if again, ok := ParseNotificationOrigin(encoded); !ok || again != o {
			t.Fatal("accepted origin does not round trip")
		}
	})
}
