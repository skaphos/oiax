package reconcile

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// AnnotationStyle selects the CI host dialect annotations are written in.
type AnnotationStyle int

const (
	// AnnotateGitHub emits GitHub Actions workflow commands
	// (::warning:: / ::error::). The zero value, matching the first
	// supported host.
	AnnotateGitHub AnnotationStyle = iota
	// AnnotateAzurePipelines emits Azure Pipelines logging commands
	// (##vso[task.logissue type=warning|error]).
	AnnotateAzurePipelines
)

// NewLogger builds the structured logger. format selects the handler:
// "json" yields a JSON handler, anything else a text handler, both writing
// to logOut (stderr in production). When annOut is non-nil, records at WARN
// and ERROR additionally emit CI annotations to it in the given style so
// they surface in the job UI; pass a nil annOut outside CI to disable
// annotations. Credential values are never logged by this package.
func NewLogger(format string, style AnnotationStyle, annOut, logOut io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	var base slog.Handler
	if strings.EqualFold(format, "json") {
		base = slog.NewJSONHandler(logOut, opts)
	} else {
		base = slog.NewTextHandler(logOut, opts)
	}
	if annOut != nil {
		base = &annotationHandler{Handler: base, style: style, out: annOut}
	}
	return slog.New(base)
}

// annotationHandler decorates a base slog.Handler: WARN and ERROR records
// also emit CI annotations to out — GitHub Actions workflow commands or
// Azure Pipelines logging commands, per style — so operator-facing
// warnings (a degraded token, a reported divergence) show up in the job
// summary. The record still flows to the base handler for the structured
// log line.
type annotationHandler struct {
	slog.Handler
	style AnnotationStyle
	out   io.Writer
}

func (h *annotationHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		fmt.Fprint(h.out, formatAnnotation(h.style, r.Level, r.Message))
	}
	return h.Handler.Handle(ctx, r)
}

// formatAnnotation renders one annotation line in the host's dialect. The
// message is escaped so it can never break out of the single command line
// (see escapeAnnotation / escapeAzureAnnotation).
func formatAnnotation(style AnnotationStyle, level slog.Level, msg string) string {
	severity := "warning"
	if level >= slog.LevelError {
		severity = "error"
	}
	if style == AnnotateAzurePipelines {
		return fmt.Sprintf("##vso[task.logissue type=%s]%s\n", severity, escapeAzureAnnotation(msg))
	}
	return fmt.Sprintf("::%s::%s\n", severity, escapeAnnotation(msg))
}

// escapeAnnotation percent-encodes the characters GitHub Actions treats
// specially in a workflow-command message: '%', CR, and LF. Without this a
// message containing a newline would truncate the annotation or let a
// crafted message inject a further ::workflow:: command. '%' is escaped
// first so the encodings it introduces are not re-escaped.
func escapeAnnotation(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	return s
}

// escapeAzureAnnotation encodes the message ("data") section of an Azure
// Pipelines logging command the way the agent's decoder expects: '%'
// becomes the agent-specific %AZP25 token, CR and LF percent-encode. As
// with GitHub, an unescaped newline would let a crafted message inject a
// further ##vso command line. '%' is escaped first so the encodings it
// introduces are not re-escaped.
func escapeAzureAnnotation(s string) string {
	s = strings.ReplaceAll(s, "%", "%AZP25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	return s
}

// WithAttrs and WithGroup re-wrap so the annotation behavior survives
// derived loggers (the embedded handler's methods would otherwise return
// the undecorated base).
func (h *annotationHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &annotationHandler{Handler: h.Handler.WithAttrs(attrs), style: h.style, out: h.out}
}

func (h *annotationHandler) WithGroup(name string) slog.Handler {
	return &annotationHandler{Handler: h.Handler.WithGroup(name), style: h.style, out: h.out}
}
