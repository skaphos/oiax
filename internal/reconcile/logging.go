package reconcile

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/skaphos/oiax/v2/internal/textsafe"
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

// Handle neutralizes host command openers in the record before either
// output (spec 002 research R6): the message and every string-valued
// attribute pass through textsafe.LogCommand, so neither the annotation nor
// the plain log line can carry "::", "##vso[", or "##[" from untrusted text
// (a commit subject, a forge API error). Both hosts scan every captured
// line, so this applies at every level, not only to annotations.
func (h *annotationHandler) Handle(ctx context.Context, r slog.Record) error {
	clean := slog.NewRecord(r.Time, r.Level, textsafe.LogCommand(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		clean.AddAttrs(neutralizeAttr(a))
		return true
	})
	if clean.Level >= slog.LevelWarn {
		fmt.Fprint(h.out, formatAnnotation(h.style, clean.Level, clean.Message))
	}
	return h.Handler.Handle(ctx, clean)
}

// neutralizeAttr applies textsafe.LogCommand to an attribute's text: string
// values, groups recursively, and any other value whose printed form would
// change (an error, a Stringer). A value that needs no change keeps its
// kind, so structured output is unchanged for ordinary attributes.
func neutralizeAttr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, textsafe.LogCommand(v.String()))
	case slog.KindGroup:
		group := v.Group()
		out := make([]slog.Attr, len(group))
		for i, g := range group {
			out[i] = neutralizeAttr(g)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	case slog.KindAny:
		printed := fmt.Sprint(v.Any())
		if safe := textsafe.LogCommand(printed); safe != printed {
			return slog.String(a.Key, safe)
		}
	}
	return slog.Attr{Key: a.Key, Value: v}
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

// EscapeAzureCommandData encodes the data section of an Azure Pipelines
// logging command (an annotation message, an uploadsummary path) the way
// the agent's decoder expects: '%' becomes the agent-specific %AZP25 token,
// CR and LF percent-encode. As with GitHub, an unescaped newline would let
// a crafted value inject a further ##vso command line. '%' is escaped first
// so the encodings it introduces are not re-escaped.
func EscapeAzureCommandData(s string) string {
	s = strings.ReplaceAll(s, "%", "%AZP25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	return s
}

// escapeAzureAnnotation encodes an Azure annotation message.
func escapeAzureAnnotation(s string) string { return EscapeAzureCommandData(s) }

// WithAttrs and WithGroup re-wrap so the annotation behavior survives
// derived loggers (the embedded handler's methods would otherwise return
// the undecorated base).
func (h *annotationHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		clean[i] = neutralizeAttr(a)
	}
	return &annotationHandler{Handler: h.Handler.WithAttrs(clean), style: h.style, out: h.out}
}

func (h *annotationHandler) WithGroup(name string) slog.Handler {
	return &annotationHandler{Handler: h.Handler.WithGroup(name), style: h.style, out: h.out}
}
