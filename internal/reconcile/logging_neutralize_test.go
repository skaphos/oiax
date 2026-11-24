package reconcile

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// commandOpeners are the host command introducers no log output may carry
// from untrusted text (spec 002 research R6).
var commandOpeners = []string{"##vso[", "##[", "::warning", "::error", "::set-output", "::stop-commands"}

// assertNoOpeners fails when out holds a command opener anywhere other than
// the annotation command Oiax itself writes at the start of a line.
func assertNoOpeners(t *testing.T, what, out string) {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		body := line
		for _, own := range []string{"::warning::", "::error::", "##vso[task.logissue type=warning]", "##vso[task.logissue type=error]"} {
			body = strings.TrimPrefix(body, own)
		}
		low := strings.ToLower(body)
		for _, bad := range commandOpeners {
			if strings.Contains(low, bad) {
				t.Fatalf("%s carries %q:\n%s", what, bad, line)
			}
		}
	}
}

// Untrusted text in a warning's message or attributes cannot inject a host
// command into the annotation or the plain log copy, on either host (T037).
func TestLoggerNeutralizesCommandsInMessagesAndAttrs(t *testing.T) {
	const hostile = "x ##vso[task.setvariable variable=OIAX_INJECTED]1 ##[error]y ::warning::z"
	for _, tt := range []struct {
		name   string
		style  AnnotationStyle
		format string
	}{
		{"github text", AnnotateGitHub, "text"},
		{"github json", AnnotateGitHub, "json"},
		{"azure text", AnnotateAzurePipelines, "text"},
		{"azure json", AnnotateAzurePipelines, "json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logOut, annOut bytes.Buffer
			logger := NewLogger(tt.format, tt.style, &annOut, &logOut)
			logger.Warn("conflict at "+hostile,
				"err", errors.New("forge said ##vso[task.complete result=Succeeded]"),
				"subject", hostile,
				slog.Group("forge", "message", "::error::from a group"))
			logger.With("bound", "##vso[task.setvariable variable=B]1").Info("info line", "detail", hostile)

			assertNoOpeners(t, "annotation", annOut.String())
			assertNoOpeners(t, "plain log", logOut.String())
			if !strings.Contains(stripWJLog(logOut.String()), "task.complete result=Succeeded") {
				t.Fatalf("error text was dropped rather than neutralized:\n%s", logOut.String())
			}
		})
	}
}

// Ordinary attribute values keep their kind and text.
func TestLoggerKeepsOrdinaryAttrs(t *testing.T) {
	var logOut, annOut bytes.Buffer
	logger := NewLogger("json", AnnotateGitHub, &annOut, &logOut)
	logger.Info("apply complete", "applied", 3, "commits", []string{"abc1234"}, "ok", true)
	out := logOut.String()
	for _, want := range []string{`"applied":3`, `"commits":["abc1234"]`, `"ok":true`} {
		if !strings.Contains(out, want) {
			t.Fatalf("log lost %s:\n%s", want, out)
		}
	}
}

func stripWJLog(s string) string { return strings.ReplaceAll(s, "\u2060", "") }
