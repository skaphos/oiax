package notification

import (
	"strings"
	"testing"

	v1 "github.com/skaphos/oiax/pkg/api/v1"
)

func templateTextPointer(s string) *string { return &s }

func TestTemplateSlots(t *testing.T) {
	p := modelPolicy()
	p.Templates = &v1.NotificationTemplates{Title: templateTextPointer("{{.Event}} {{.RequestType}}"), Body: templateTextPointer("graph body")}
	p.Destinations[0].Templates = &v1.NotificationTemplates{Body: templateTextPointer("{{.DestinationEnvironment}} {{range .Commits}}{{shortSHA .SHA}} {{trunc 4 .Subject}}{{end}}")}
	set, err := ResolveTemplates(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []v1.NotificationEvent{v1.NotificationRequestCreated, v1.NotificationRequestMerged} {
		for _, typ := range []v1.NotificationRequestType{v1.NotificationPromotion, v1.NotificationBackflow} {
			e := modelEvent()
			e.Kind, e.Request.Type = kind, typ
			e.ID = EventID(e.Repository, e.Request.ID, kind)
			e.DestinationEnvironment = "test environment"
			e.Snapshot.Commits = []CommitSummary{{SHA: strings.Repeat("a", 40), Subject: "hello"}}
			m, err := set.Render("ops", e)
			if err != nil || m.Title != string(kind)+" "+string(typ) || m.Body != "test environment aaaaaaa hell" {
				t.Fatalf("%+v: %v", m, err)
			}
		}
	}
	p.Destinations[0].Templates = &v1.NotificationTemplates{Title: templateTextPointer(""), Body: templateTextPointer("")}
	set, err = ResolveTemplates(p)
	if err != nil {
		t.Fatal(err)
	}
	m, err := set.Render("ops", modelEvent())
	if err != nil || m.Title != "" || m.Body != "" {
		t.Fatalf("empty override: %+v %v", m, err)
	}
	if facts, err := FixedFacts(modelEvent()); err != nil || !strings.Contains(facts, "Request ID: 42") || !strings.Contains(facts, "Observed at: ") {
		t.Fatal(facts, err)
	}
}

func TestTemplateValidation(t *testing.T) {
	for _, source := range []string{
		"{{.Secret}}", "{{env `SECRET`}}", "{{now}}", "{{exec `echo`}}",
		"{{if false}}{{.Secret}}{{end}}", "{{range .Commits}}{{.Secret}}{{end}}",
		"{{.OccurredAt.AddDate 1 1 1}}", "{{template `hidden` .}}", "{{range 1000000000}}{{end}}",
		"{{if eq .Event `request-created`}}{{if eq .RequestType `backflow`}}{{index .Commits 999}}{{end}}{{end}}",
		strings.Repeat("x", (12<<10)+1), strings.Repeat("x", (1<<20)+1),
	} {
		p := modelPolicy()
		p.Templates = &v1.NotificationTemplates{Body: &source}
		if _, err := ResolveTemplates(p); err == nil || strings.Contains(err.Error(), source) {
			t.Fatalf("unsafe or unredacted validation: %v", err)
		}
	}
	p := modelPolicy()
	p.Templates = &v1.NotificationTemplates{Title: templateTextPointer(strings.Repeat("é", 300) + "\n"), Body: templateTextPointer("safe\x1b\u202e")}
	set, err := ResolveTemplates(p)
	if err != nil {
		t.Fatal(err)
	}
	m, err := set.Render("ops", modelEvent())
	if err != nil || len([]rune(m.Title)) != 256 || strings.ContainsAny(m.Body, "\x1b\u202e") {
		t.Fatalf("%+v %v", m, err)
	}
}

func FuzzNotificationTemplate(f *testing.F) {
	for _, s := range []string{"{{.Event}}", "{{range .Commits}}{{.Subject}}{{end}}", "{{.Secret}}", "{{if false}}{{.Missing}}{{end}}"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 4096 {
			t.Skip()
		}
		p := modelPolicy()
		p.Templates = &v1.NotificationTemplates{Body: &source}
		set, err := ResolveTemplates(p)
		if err == nil {
			message, err := set.Render("ops", modelEvent())
			if err == nil && len(message.Body) > 12<<10 {
				t.Fatal("unbounded body")
			}
		}
	})
}
