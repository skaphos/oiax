package github

import (
	"errors"
	"strings"
	"testing"
)

func TestAPIErrorMessage(t *testing.T) {
	t.Parallel()
	e := &apiError{StatusCode: 422, Message: "Validation Failed"}
	if got := e.Error(); got != "github api 422: Validation Failed" {
		t.Errorf("Error() = %q", got)
	}
	e = &apiError{
		StatusCode: 422,
		Message:    "Validation Failed",
		Details:    []string{"a pull request already exists", "base invalid"},
	}
	want := "github api 422: Validation Failed (a pull request already exists; base invalid)"
	if got := e.Error(); got != want {
		t.Errorf("Error() with details = %q, want %q", got, want)
	}
}

func TestErrNoResponseWraps(t *testing.T) {
	t.Parallel()
	cause := errors.New("connection reset")
	e := &errNoResponse{err: cause}
	if got := e.Error(); got != "connection reset" {
		t.Errorf("Error() = %q, want the cause verbatim", got)
	}
	if !errors.Is(e, cause) {
		t.Error("errors.Is(errNoResponse, cause) = false, want true via Unwrap")
	}
}

func TestScrubToken(t *testing.T) {
	t.Parallel()
	p := &Provider{Token: "s3cr3t"}
	got := p.scrubToken("push https://x-access-token:s3cr3t@github.com failed")
	if strings.Contains(got, "s3cr3t") {
		t.Errorf("scrubToken left the credential in %q", got)
	}
	if !strings.Contains(got, tokenRedaction) {
		t.Errorf("scrubToken output %q lacks the redaction placeholder", got)
	}
	// Without a token there is nothing to scrub — the input passes through.
	p = &Provider{}
	if got := p.scrubToken("unchanged"); got != "unchanged" {
		t.Errorf("scrubToken with no token = %q, want unchanged", got)
	}
}

func TestBaseURL(t *testing.T) {
	t.Parallel()
	p := &Provider{}
	if got := p.baseURL(); got != defaultBaseURL {
		t.Errorf("baseURL() = %q, want the default %q", got, defaultBaseURL)
	}
	p = &Provider{BaseURL: "https://ghe.example.com/api/v3"}
	if got := p.baseURL(); got != "https://ghe.example.com/api/v3" {
		t.Errorf("baseURL() = %q, want the override", got)
	}
}
