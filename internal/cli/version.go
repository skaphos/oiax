package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/skaphos/oiax/internal/version"
)

// versionInfo is the JSON shape of `oiax version -o json` / `oiax --version
// -o json`. Field names are stable and part of the compatibility contract:
// scripts grep or unmarshal this, so they must not change shape across
// releases.
type versionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

func newVersionCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return printVersion(cmd, opts)
		},
	}
}

// printVersion renders build version information in the selected output
// format. The text form's first line is always "oiax <version>" — stable
// and greppable — with commit and build date on their own labeled lines so
// each is greppable independently too.
func printVersion(cmd *cobra.Command, opts *options) error {
	if opts.output == "json" {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(versionInfo{
			Version: version.Version,
			Commit:  version.Commit,
			Date:    version.Date,
		})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "oiax %s\ncommit: %s\nbuilt: %s\n",
		version.Version, version.Commit, version.Date)
	return nil
}
