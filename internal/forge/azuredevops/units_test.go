package azuredevops

import (
	"errors"
	"strings"
	"testing"
)

func TestRepoString(t *testing.T) {
	t.Parallel()
	r := Repo{Organization: "acme", Project: "platform", Name: "oiax"}
	if got := r.String(); got != "acme/platform/oiax" {
		t.Errorf("String() = %q, want acme/platform/oiax", got)
	}
}

func TestErrNoResponseWraps(t *testing.T) {
	t.Parallel()
	cause := errors.New("dial tcp: connection refused")
	e := &errNoResponse{err: cause}
	if got := e.Error(); got != "dial tcp: connection refused" {
		t.Errorf("Error() = %q, want the cause verbatim", got)
	}
	if !errors.Is(e, cause) {
		t.Error("errors.Is(errNoResponse, cause) = false, want true via Unwrap")
	}
}

func TestScrubToken(t *testing.T) {
	t.Parallel()
	p := &Provider{Token: "s3cr3t"}
	got := p.scrubToken("fetch https://pat:s3cr3t@dev.azure.com failed")
	if strings.Contains(got, "s3cr3t") {
		t.Errorf("scrubToken left the credential in %q", got)
	}
	if !strings.Contains(got, tokenRedaction) {
		t.Errorf("scrubToken output %q lacks the redaction placeholder", got)
	}
	p = &Provider{}
	if got := p.scrubToken("unchanged"); got != "unchanged" {
		t.Errorf("scrubToken with no token = %q, want unchanged", got)
	}
}
