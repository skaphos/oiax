package config

import (
	"strings"
	"testing"
)

const notificationConfigPrefix = "apiVersion: oiax.skaphos.dev/v1\nkind: PromotionGraph\nmetadata: {name: example}\nspec:\n  branches: {development: {}, test: {}}\n  promotions: [{from: development, to: test}]\n"

func TestNotificationStrictParsing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, extra string
		fail        bool
	}{
		{"old config", "", false},
		{"defaults", "  notifications:\n    destinations: [{name: ops, type: teams, endpointEnv: TEAMS}]\n", false},
		{"empty selections", "  notifications:\n    destinations: [{name: ops, type: webhook, endpointEnv: AUDIT, events: [], requestTypes: [], enabled: false}]\n", false},
		{"unknown policy", "  notifications: {unknown: true}\n", true},
		{"unknown destination", "  notifications:\n    destinations: [{name: ops, type: teams, endpointEnv: TEAMS, url: secret}]\n", true},
		{"unknown template", "  notifications:\n    templates: {command: secret}\n", true},
		{"duplicate keys", "  notifications: {destinations: [], destinations: []}\n", true},
	} {
		for _, version := range []string{"v1", "v1alpha1"} {
			t.Run(tc.name+version, func(t *testing.T) {
				t.Parallel()
				data := strings.Replace(notificationConfigPrefix, "/v1\n", "/"+version+"\n", 1) + tc.extra
				g, err := Parse([]byte(data))
				if (err != nil) != tc.fail {
					t.Fatalf("Parse: %v", err)
				}
				if err == nil {
					g.Default()
					if errs := g.Validate(); len(errs) != 0 {
						t.Fatal(errs)
					}
				}
			})
		}
	}
}

func TestNotificationParseErrorsDoNotEchoSecrets(t *testing.T) {
	t.Parallel()
	secret := "https://secret.invalid/private-token"
	for _, extra := range []string{
		"  notifications:\n    destinations: [{enabled: '" + secret + "'}]\n",
		"  notifications:\n    destinations: '" + secret + "'\n",
		"  notifications:\n    environmentNames: {test: ['" + secret + "']}\n",
	} {
		_, err := Parse([]byte(notificationConfigPrefix + extra))
		if err == nil {
			t.Fatal("invalid type accepted")
		}
		if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "https://") {
			t.Fatalf("parse error leaked rejected value: %v", err)
		}
	}
}
