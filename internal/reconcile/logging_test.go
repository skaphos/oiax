package reconcile

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewLoggerAnnotatesWarningsOnlyWhenSinkSet(t *testing.T) {
	var logOut, annOut bytes.Buffer
	logger := NewLogger("text", &annOut, &logOut)

	logger.Info("just info")
	logger.Warn("something degraded")
	logger.Error("something failed")

	ann := annOut.String()
	if !strings.Contains(ann, "::warning::something degraded") {
		t.Errorf("missing warning annotation:\n%s", ann)
	}
	if !strings.Contains(ann, "::error::something failed") {
		t.Errorf("missing error annotation:\n%s", ann)
	}
	if strings.Contains(ann, "just info") {
		t.Errorf("info must not be annotated:\n%s", ann)
	}
	if !strings.Contains(logOut.String(), "something degraded") {
		t.Errorf("structured log missing warning:\n%s", logOut.String())
	}
}

func TestAnnotationEscapesWorkflowCommandChars(t *testing.T) {
	var logOut, annOut bytes.Buffer
	logger := NewLogger("text", &annOut, &logOut)

	// A message with a newline, a percent, and an embedded workflow command.
	logger.Warn("line1\nline2 is 100% ::error:: sneaky")

	ann := strings.TrimRight(annOut.String(), "\n")
	if strings.Contains(ann, "\n") {
		t.Fatalf("annotation must stay one line; unescaped newline leaked:\n%q", ann)
	}
	if !strings.HasPrefix(ann, "::warning::") {
		t.Fatalf("expected ::warning:: prefix:\n%q", ann)
	}
	if !strings.Contains(ann, "line1%0Aline2") {
		t.Errorf("newline not escaped to %%0A:\n%q", ann)
	}
	if !strings.Contains(ann, "100%25") {
		t.Errorf("percent not escaped to %%25:\n%q", ann)
	}
}

func TestNewLoggerNoAnnotationSink(t *testing.T) {
	var logOut bytes.Buffer
	logger := NewLogger("json", nil, &logOut)
	logger.Warn("degraded")

	out := logOut.String()
	if strings.Contains(out, "::warning::") {
		t.Errorf("no annotations expected without a sink:\n%s", out)
	}
	// JSON handler emits structured lines.
	if !strings.Contains(out, `"msg":"degraded"`) {
		t.Errorf("expected json log line:\n%s", out)
	}
}
