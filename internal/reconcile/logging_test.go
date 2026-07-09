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
