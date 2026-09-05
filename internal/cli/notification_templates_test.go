package cli

import (
	"strings"
	"testing"
	"time"
)

func TestNotificationTemplatesBinary(t *testing.T) {
	t.Parallel()
	binary := buildNotificationBinary(t)
	for _, provider := range []string{"github", "azuredevops"} {
		t.Run(provider, func(t *testing.T) {
			f := newNotificationBinaryFixture(t, binary, provider, "webhook", "    environmentNames: {test: Testing, dev: Development}", "    templates:", "      title: '{{.Event}} {{.RequestType}}'", "      body: 'These commits were promoted to the {{.DestinationEnvironment}} environment.'")
			f.run(0, "reconcile")
			f.setSeeds(f.seed(42, "promotion", "merged", time.Now().UTC(), true))
			f.run(0, "reconcile")
			messages := f.messages()
			if len(messages) != 1 {
				t.Fatalf("messages=%d", len(messages))
			}
			body := string(messages[0].Body)
			for _, want := range []string{"These commits were promoted to the Testing environment.", "Request ID: 42", "Observed at:", "Commit details unavailable", "request-merged promotion"} {
				if !strings.Contains(body, want) {
					t.Fatalf("missing %q", want)
				}
			}
			f.run(0, "reconcile")
			if len(f.messages()) != 1 {
				t.Fatal("repeated notification")
			}
		})
	}
}
