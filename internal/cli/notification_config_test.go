package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/skaphos/oiax/internal/forge"
	"github.com/skaphos/oiax/internal/gittest"
	"github.com/skaphos/oiax/internal/notification"
)

func TestNotificationLoadedConfigPinsFiles(t *testing.T) {
	// The existing CLI resolves paths relative to process cwd; Chdir is serial.
	dir := t.TempDir()
	gittest.InitRepo(t, dir)
	t.Chdir(dir)
	configText := "apiVersion: oiax.skaphos.dev/v1\nkind: PromotionGraph\nmetadata: {name: graph}\nspec:\n  branches: {development: {}, test: {}}\n  promotions: [{from: development, to: test}]\n  notifications:\n    templates: {bodyFile: message.txt}\n    destinations: [{name: ops, type: slack, endpointEnv: SLACK}]\n"
	for name, body := range map[string]string{".oiax.yaml": configText, "message.txt": "Pinned promotion wording"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gittest.Run(t, dir, "add", ".")
	gittest.Run(t, dir, "commit", "-qm", "config")
	oid := gittest.Run(t, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "message.txt"), []byte("UNTRUSTED LOCAL"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetErr(&bytes.Buffer{})
	loaded, err := loadGraph(cmd, &options{configPath: ".oiax.yaml"}, "main")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ConfigOID != oid || loaded.Notifications == nil || *loaded.Notifications.Templates.Body != "Pinned promotion wording" {
		t.Fatalf("not pinned: %+v", loaded)
	}
	if loaded.Notifications.Templates.BodyFile != "" {
		t.Fatal("unresolved file path retained")
	}
	if len(loaded.NotificationSources) != 1 {
		t.Fatal("sources not retained separately")
	}
	if gittest.Run(t, dir, "rev-parse", "HEAD") != oid {
		t.Fatal("read changed repository")
	}
}

func TestNotificationLoadedConfigRejectsOversizedSource(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	configText := "apiVersion: oiax.skaphos.dev/v1\nkind: PromotionGraph\nmetadata: {name: graph}\nspec:\n  branches: {development: {}}\n  notifications:\n    templates: {bodyFile: message.txt}\n"
	if err := os.WriteFile(".oiax.yaml", []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("message.txt", []byte(strings.Repeat("a", (1<<20)+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetErr(&bytes.Buffer{})
	if _, err := loadGraph(cmd, &options{configPath: ".oiax.yaml"}, ""); err == nil {
		t.Fatal("oversized source accepted")
	}
}

type notificationCapabilityTrap struct {
	*fakeForge
	t *testing.T
}

func (f *notificationCapabilityTrap) RepositoryIdentity(context.Context) (notification.RepositoryIdentity, error) {
	f.t.Fatal("disabled policy called notification identity capability")
	return notification.RepositoryIdentity{}, nil
}
func (f *notificationCapabilityTrap) ListLifecyclePage(context.Context, forge.LifecycleQuery) (forge.LifecyclePage, error) {
	f.t.Fatal("disabled policy scanned lifecycle")
	return forge.LifecyclePage{}, nil
}
func (f *notificationCapabilityTrap) GetLifecycleRequest(context.Context, forge.RequestID) (notification.LifecycleRequest, error) {
	f.t.Fatal("disabled policy read lifecycle details")
	return notification.LifecycleRequest{}, nil
}

func TestNotificationDisabledPreservesLegacyOutput(t *testing.T) {
	run := setupRepo(t)
	useForge(t, &notificationCapabilityTrap{fakeForge: &fakeForge{}, t: t})
	stdout := func(command, format string) string {
		t.Helper()
		cmd := NewRootCommand()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{command, "--output", format})
		if err := cmd.ExecuteContext(context.Background()); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	for _, command := range []string{"plan", "reconcile"} {
		for _, format := range []string{"text", "json"} {
			run("write", ".oiax.yaml", exampleConfig)
			before := stdout(command, format)
			for _, destination := range []string{
				"[]",
				"[{name: ops, type: slack, endpointEnv: NEVER_READ, enabled: false}]",
				"[{name: ops, type: teams, endpointEnv: NEVER_READ, events: []}]",
				"[{name: ops, type: webhook, endpointEnv: NEVER_READ, requestTypes: []}]",
			} {
				run("write", ".oiax.yaml", exampleConfig+"\n  notifications:\n    destinations: "+destination+"\n")
				after := stdout(command, format)
				if before != after {
					t.Fatalf("disabled policy changed %s/%s: before %q after %q", command, format, before, after)
				}
			}
		}
	}
}
