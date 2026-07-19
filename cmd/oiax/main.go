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
	// SIGINT (Ctrl-C) or SIGTERM cancels the root context so an in-flight
	// reconcile — a git subprocess or a Retry-After backoff, both already
	// context-aware — unwinds promptly instead of running to completion after
	// the operator asked it to stop. Execute threads this context to every
	// command via cobra's ExecuteContext; stop() restores default signal
	// handling before the process exits.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Execute(ctx, os.Args[1:])
	stop()
	os.Exit(code)
}
