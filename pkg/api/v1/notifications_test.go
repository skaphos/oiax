package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func notificationPolicy() *NotificationPolicy {
	return &NotificationPolicy{Destinations: []NotificationDestination{{Name: "operations", Type: "slack", EndpointEnv: "OIAX_SLACK"}}}
}

func TestNotificationDefaultsAndRoundTrip(t *testing.T) {
	t.Parallel()
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprint(explicit), func(t *testing.T) {
			t.Parallel()
			g := validGraph()
			g.Spec.Notifications = notificationPolicy()
			d := &g.Spec.Notifications.Destinations[0]
			if explicit {
				d.Enabled = new(bool)
				d.Events = []NotificationEvent{}
				d.RequestTypes = []NotificationRequestType{}
				empty := ""
				d.Templates = &NotificationTemplates{Title: &empty, Body: &empty}
			}
			if errs := g.Validate(); len(errs) != 0 {
				t.Fatal(errs)
			}
			// Round-trip before defaulting as well: omission must not become [].
			for _, codec := range []struct {
				name      string
				marshal   func(any) ([]byte, error)
				unmarshal func([]byte, any) error
			}{{"json", json.Marshal, json.Unmarshal}, {"yaml", yaml.Marshal, yaml.Unmarshal}} {
				data, err := codec.marshal(g)
				if err != nil {
					t.Fatal(err)
				}
				var out PromotionGraph
				if err := codec.unmarshal(data, &out); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(g, &out) {
					t.Fatalf("%s changed omission semantics: %s", codec.name, data)
				}
				out.Default()
				got := out.Spec.Notifications.Destinations[0]
				if explicit {
					if *got.Enabled || got.Events == nil || len(got.Events) != 0 || got.RequestTypes == nil || len(got.RequestTypes) != 0 {
						t.Fatalf("explicit choices lost: %+v", got)
					}
				} else if !*got.Enabled || !reflect.DeepEqual(got.Events, []NotificationEvent{"request-merged"}) || !reflect.DeepEqual(got.RequestTypes, []NotificationRequestType{"promotion", "backflow"}) {
					t.Fatalf("wrong defaults: %+v", got)
				}
				before, _ := json.Marshal(out)
				out.Default()
				after, _ := json.Marshal(out)
				if string(before) != string(after) {
					t.Fatal("Default is not idempotent")
				}
				if errs := out.Validate(); len(errs) != 0 {
					t.Fatal(errs)
				}
			}
		})
	}
}

func TestNotificationValidation(t *testing.T) {
	t.Parallel()
	secret := "https://secret.invalid/private-token"
	for _, tc := range []struct {
		name, path string
		change     func(*NotificationPolicy)
	}{
		{"duplicate names", "destinations[1].name", func(p *NotificationPolicy) { p.Destinations = append(p.Destinations, p.Destinations[0]) }},
		{"too many", "destinations", func(p *NotificationPolicy) {
			for i := range 20 {
				d := p.Destinations[0]
				d.Name = fmt.Sprintf("d-%d", i)
				p.Destinations = append(p.Destinations, d)
			}
		}},
		{"bad name", "destinations[0].name", func(p *NotificationPolicy) { p.Destinations[0].Name = secret }},
		{"long name", "destinations[0].name", func(p *NotificationPolicy) { p.Destinations[0].Name = strings.Repeat("a", 64) }},
		{"email", "destinations[0].type", func(p *NotificationPolicy) { p.Destinations[0].Type = "email" }},
		{"inline endpoint", "destinations[0].endpointEnv", func(p *NotificationPolicy) { p.Destinations[0].EndpointEnv = secret }},
		{"private slack", "destinations[0].allowPrivateNetwork", func(p *NotificationPolicy) { p.Destinations[0].AllowPrivateNetwork = true }},
		{"unknown event", "destinations[0].events[0]", func(p *NotificationPolicy) { p.Destinations[0].Events = []NotificationEvent{NotificationEvent(secret)} }},
		{"duplicate event", "destinations[0].events[1]", func(p *NotificationPolicy) {
			p.Destinations[0].Events = []NotificationEvent{"request-merged", "request-merged"}
		}},
		{"unknown type", "destinations[0].requestTypes[0]", func(p *NotificationPolicy) {
			p.Destinations[0].RequestTypes = []NotificationRequestType{NotificationRequestType(secret)}
		}},
		{"duplicate type", "destinations[0].requestTypes[1]", func(p *NotificationPolicy) {
			p.Destinations[0].RequestTypes = []NotificationRequestType{"promotion", "promotion"}
		}},
		{"unknown branch", "environmentNames", func(p *NotificationPolicy) { p.EnvironmentNames = map[string]string{secret: "test"} }},
		{"empty label", "environmentNames", func(p *NotificationPolicy) { p.EnvironmentNames = map[string]string{"test": ""} }},
		{"long label", "environmentNames", func(p *NotificationPolicy) { p.EnvironmentNames = map[string]string{"test": strings.Repeat("é", 101)} }},
		{"control label", "environmentNames", func(p *NotificationPolicy) { p.EnvironmentNames = map[string]string{"test": "test\nchannel"} }},
		{"body conflict", "templates", func(p *NotificationPolicy) {
			body := ""
			p.Templates = &NotificationTemplates{Body: &body, BodyFile: "body.txt"}
		}},
		{"traversal", "templates.bodyFile", func(p *NotificationPolicy) { p.Templates = &NotificationTemplates{BodyFile: "../body"} }},
		{"absolute", "templates.bodyFile", func(p *NotificationPolicy) { p.Templates = &NotificationTemplates{BodyFile: "/body"} }},
		{"windows absolute", "templates.bodyFile", func(p *NotificationPolicy) { p.Templates = &NotificationTemplates{BodyFile: "C:/body"} }},
		{"secret file", "templates.bodyFile", func(p *NotificationPolicy) { p.Destinations[0].Templates = &NotificationTemplates{BodyFile: secret} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := validGraph()
			g.Spec.Notifications = notificationPolicy()
			tc.change(g.Spec.Notifications)
			err := errors.Join(g.Validate()...)
			if err == nil || !strings.Contains(err.Error(), tc.path) {
				t.Fatalf("got %v, want field %s", err, tc.path)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatal("rejected value leaked")
			}
		})
	}
}

func TestNotificationPolicyEnabled(t *testing.T) {
	t.Parallel()
	var absent *NotificationPolicy
	if absent.IsEnabled() {
		t.Fatal("nil policy enabled")
	}
	p := notificationPolicy()
	if !p.IsEnabled() {
		t.Fatal("omitted selections should enable merge notifications")
	}
	p.Destinations[0].Events = []NotificationEvent{}
	if p.IsEnabled() {
		t.Fatal("empty events enabled")
	}
	p.Destinations[0].Events = nil
	p.Destinations[0].RequestTypes = []NotificationRequestType{}
	if p.IsEnabled() {
		t.Fatal("empty types enabled")
	}
	p.Destinations[0].RequestTypes = nil
	p.Destinations[0].Enabled = new(bool)
	if p.IsEnabled() {
		t.Fatal("explicit false enabled")
	}
}

func TestNotificationTemplateSourceDiagnosticPaths(t *testing.T) {
	t.Parallel()
	for _, group := range []string{"templates", "destinations[0].templates"} {
		for _, field := range []string{"title", "body"} {
			for _, invalid := range []string{strings.Repeat("x", (1<<20)+1), string([]byte{0xff})} {
				g := validGraph()
				g.Spec.Notifications = notificationPolicy()
				slots := &NotificationTemplates{}
				if field == "title" {
					slots.Title = &invalid
				} else {
					slots.Body = &invalid
				}
				if group == "templates" {
					g.Spec.Notifications.Templates = slots
				} else {
					g.Spec.Notifications.Destinations[0].Templates = slots
				}
				errs := g.Validate()
				want := "spec.notifications." + group + "." + field
				if len(errs) != 1 || !strings.Contains(errs[0].Error(), want+":") {
					t.Fatalf("got %v, want exact field %s", errs, want)
				}
			}
		}
	}
}
