// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provider

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// RunArtifactCreds are the runner-injected credentials for the
// GitHub-Actions-compatible run-artifact transport that the github and forgejo
// adapters both speak. Only the URL/token variable names differ between them,
// so the adapters supply those; the shared policy lives in
// ValidateRunArtifactCreds.
type RunArtifactCreds struct {
	Forge    string // forge id for the runtime-required message (e.g. "github")
	URLVar   string // env var carrying the endpoint URL
	URLWhat  string // human description of URLVar
	TokenVar string // env var carrying the per-job token
	URL      string // resolved URL value
	Token    string // resolved token value
}

// ValidateRunArtifactCreds enforces the shared credential policy: absent creds
// mean "not running inside a CI job" (the friendly, variable-listing runtime
// error), while present-but-malformed creds are an in-job misconfiguration (a
// terse usage error). Centralised so both adapters classify identically and a
// future hardening lands in one place.
func ValidateRunArtifactCreds(creds RunArtifactCreds) error {
	if creds.URL == "" || creds.Token == "" {
		return errs.RuntimeRequired("transfer run artifacts", creds.Forge, []errs.EnvVar{
			{Name: creds.URLVar, What: creds.URLWhat},
			{Name: creds.TokenVar, What: "the per-job artifact token"},
		})
	}

	switch {
	case !strings.HasPrefix(creds.URL, "http://") && !strings.HasPrefix(creds.URL, "https://"):
		return fmt.Errorf("%s must be an HTTP(S) URL: %w", creds.URLVar, errs.ErrUsage)
	case strings.ContainsAny(creds.Token, "\n\r"):
		return fmt.Errorf("%s must be a single-line value: %w", creds.TokenVar, errs.ErrUsage)
	}

	return nil
}
