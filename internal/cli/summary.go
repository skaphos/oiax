package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/skaphos/oiax/v2/internal/cienv"
	"github.com/skaphos/oiax/v2/internal/reconcile"
	"github.com/spf13/cobra"
)

// summaryTarget opens the destination for one CI job-summary write on the
// detected host, shared by the plan summary, the notification summary, and
// the conflict sections (spec 002 research R7):
//
//   - GitHub Actions: append to the file named by GITHUB_STEP_SUMMARY.
//   - Azure Pipelines: a new Markdown file in AGENT_TEMPDIRECTORY (named by
//     pattern, as for os.CreateTemp), attached by finish with an
//     ##vso[task.uploadsummary] command on stderr. The path is escaped as
//     command data, since the directory is caller-controlled.
//
// ok is false when there is no summary destination (no CI host, or the file
// cannot be opened; an open failure is reported on stderr). Otherwise the
// caller writes to w and must call finish with the first write error, if
// any: finish closes the file and, on Azure, attaches it only when every
// write and the close succeeded. finish returns the write or close error.
func summaryTarget(cmd *cobra.Command, pattern string) (w io.Writer, finish func(writeErr error) error, ok bool) {
	var f *os.File
	var err error
	azure := false
	switch path := os.Getenv("GITHUB_STEP_SUMMARY"); {
	case path != "":
		f, err = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	case cienv.Detect() == cienv.AzurePipelines:
		azure = true
		f, err = os.CreateTemp(os.Getenv("AGENT_TEMPDIRECTORY"), pattern)
	default:
		return nil, nil, false
	}
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "oiax: step summary: %v\n", err)
		return nil, nil, false
	}
	finish = func(writeErr error) error {
		closeErr := f.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			return err
		}
		if azure {
			fmt.Fprintf(cmd.ErrOrStderr(), "##vso[task.uploadsummary]%s\n", reconcile.EscapeAzureCommandData(f.Name()))
		}
		return nil
	}
	return f, finish, true
}
