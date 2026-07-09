// Command oiax is the declarative Git branch promotion reconciler.
package main

import (
	"os"

	"github.com/skaphos/oiax/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:]))
}
