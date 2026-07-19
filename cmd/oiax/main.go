// Command oiax is the declarative Git branch promotion reconciler.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/skaphos/oiax/internal/cli"
)

func main() {
	// os.Exit is kept out of run so run's deferred stop() runs on every return
	// path — including a panic unwinding through it — before the process exits.
	os.Exit(run())
}

func run() int {
	// SIGINT (Ctrl-C) or SIGTERM cancels the root context so an in-flight
	// reconcile — a git subprocess or a Retry-After backoff, both already
	// context-aware — unwinds promptly instead of running to completion after
	// the operator asked it to stop. Execute threads this context to every
	// command via cobra's ExecuteContext; the deferred stop() restores default
	// signal handling on the way out.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cli.Execute(ctx, os.Args[1:])
}
