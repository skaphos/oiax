package reconcile

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// NewLogger builds the structured logger. format selects the handler:
// "json" yields a JSON handler, anything else a text handler, both writing
// to logOut (stderr in production). When annOut is non-nil, records at WARN
// and ERROR additionally emit GitHub Actions ::warning::/::error:: workflow
// commands to it (stdout under Actions) so they surface in the job UI; pass
// a nil annOut outside Actions to disable annotations. Credential values
// are never logged by this package.
func NewLogger(format string, annOut, logOut io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	var base slog.Handler
	if strings.EqualFold(format, "json") {
		base = slog.NewJSONHandler(logOut, opts)
	} else {
		base = slog.NewTextHandler(logOut, opts)
	}
	if annOut != nil {
		base = &annotationHandler{Handler: base, out: annOut}
	}
	return slog.New(base)
}

// annotationHandler decorates a base slog.Handler: WARN and ERROR records
// also emit GitHub Actions workflow annotations to out, so operator-facing
// warnings (a degraded token, a reported divergence) show up in the job
// summary. The record still flows to the base handler for the structured
// log line.
type annotationHandler struct {
	slog.Handler
	out io.Writer
}

func (h *annotationHandler) Handle(ctx context.Context, r slog.Record) error {
	switch {
	case r.Level >= slog.LevelError:
		fmt.Fprintf(h.out, "::error::%s\n", escapeAnnotation(r.Message))
	case r.Level >= slog.LevelWarn:
		fmt.Fprintf(h.out, "::warning::%s\n", escapeAnnotation(r.Message))
	}
	return h.Handler.Handle(ctx, r)
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

// WithAttrs and WithGroup re-wrap so the annotation behavior survives
// derived loggers (the embedded handler's methods would otherwise return
// the undecorated base).
func (h *annotationHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &annotationHandler{Handler: h.Handler.WithAttrs(attrs), out: h.out}
}

func (h *annotationHandler) WithGroup(name string) slog.Handler {
	return &annotationHandler{Handler: h.Handler.WithGroup(name), out: h.out}
}
