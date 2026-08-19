// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provider_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// The shared run-artifact credential policy had no test of its own --
// only incidental exercise through the two adapters that call it, which
// is precisely what it exists to keep identical.
//
// The two error classes are deliberately different, and that difference
// is the contract: absent credentials mean "not running inside a CI job"
// and get the friendly variable-listing runtime error, while present but
// malformed credentials are an in-job misconfiguration and get a usage
// error.

func runArtifactCreds() provider.RunArtifactCreds {
	return provider.RunArtifactCreds{
		Forge:    "github",
		URLVar:   "ACTIONS_RESULTS_URL",
		URLWhat:  "the run-artifact endpoint",
		TokenVar: "ACTIONS_RUNTIME_TOKEN",
		URL:      "https://results.example/",
		Token:    "runtime-token",
	}
}

func TestValidateRunArtifactCreds_WellFormed(t *testing.T) {
	t.Parallel()

	if err := provider.ValidateRunArtifactCreds(runArtifactCreds()); err != nil {
		t.Fatal(err)
	}

	// http is accepted alongside https: a self-hosted runner may talk to
	// its forge over plain HTTP on a private network.
	plain := runArtifactCreds()
	plain.URL = "http://results.internal/"

	if err := provider.ValidateRunArtifactCreds(plain); err != nil {
		t.Errorf("http URL rejected: %v", err)
	}
}

func TestValidateRunArtifactCreds_AbsentIsARuntimeError(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		mutate func(*provider.RunArtifactCreds)
	}{
		{name: "no url", mutate: func(c *provider.RunArtifactCreds) { c.URL = "" }},
		{name: "no token", mutate: func(c *provider.RunArtifactCreds) { c.Token = "" }},
		{name: "neither", mutate: func(c *provider.RunArtifactCreds) { c.URL, c.Token = "", "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			in := runArtifactCreds()
			tc.mutate(&in)

			err := provider.ValidateRunArtifactCreds(in)
			if err == nil {
				t.Fatal("expected an error")
			}

			// Not a usage error: this is the "you are not in a CI job"
			// case, and the message has to say which variables to set.
			if errors.Is(err, errs.ErrUsage) {
				t.Errorf("absent credentials classified as a usage error: %v", err)
			}

			for _, want := range []string{"ACTIONS_RESULTS_URL", "ACTIONS_RUNTIME_TOKEN"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error should name %s: %v", want, err)
				}
			}
		})
	}
}

func TestValidateRunArtifactCreds_MalformedIsAUsageError(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		mutate func(*provider.RunArtifactCreds)
	}{
		{name: "url without a scheme", mutate: func(c *provider.RunArtifactCreds) { c.URL = "results.example/" }},
		{name: "url with a hostile scheme", mutate: func(c *provider.RunArtifactCreds) { c.URL = "javascript:alert(0)" }},
		{name: "url as a file path", mutate: func(c *provider.RunArtifactCreds) { c.URL = "file:///etc/passwd" }},

		// A token carrying a line ending would split an Authorization
		// header, so it is refused rather than sent.
		{name: "token with a newline", mutate: func(c *provider.RunArtifactCreds) { c.Token = "tok\nX-Evil: 1" }},
		{name: "token with a carriage return", mutate: func(c *provider.RunArtifactCreds) { c.Token = "tok\rX-Evil: 1" }},
		{name: "token with CRLF", mutate: func(c *provider.RunArtifactCreds) { c.Token = "tok\r\nX-Evil: 1" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			in := runArtifactCreds()
			tc.mutate(&in)

			err := provider.ValidateRunArtifactCreds(in)
			if !errors.Is(err, errs.ErrUsage) {
				t.Fatalf("err = %v, want ErrUsage", err)
			}

			// The token itself must not be echoed back.
			if strings.Contains(err.Error(), in.Token) {
				t.Errorf("error echoed the token: %v", err)
			}
		})
	}
}
