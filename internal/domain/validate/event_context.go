// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// DefaultAllowedEvents lists the trigger events under which a privileged
// publish / release workflow may run with signing, package, or API
// secrets in scope. Any event outside this list is refused — most
// importantly the `pull_request*` family, where the workflow run's
// commit is contributor-controlled and a malicious caller wiring could
// otherwise leak credentials.
//
// Adopters with a legitimate PR-context publish need (preview deploys,
// staged channel pushes) override per-workflow via the --allowed-events
// flag on `reusable-ci validate event-context`. There is no env-var
// bypass — overrides must be explicit at the call site, audited like any
// other workflow input.
//
//nolint:gochecknoglobals // policy constant — read-only and ordered.
var DefaultAllowedEvents = []string{
	"push",
	"workflow_dispatch",
	"release",
	"schedule",
	"workflow_run",
	"merge_group",
}

// EventContextError is returned when the observed trigger event is not
// in the configured allowlist. The app layer formats user-facing
// guidance from the structured fields.
type EventContextError struct {
	Got     string   // observed GITHUB_EVENT_NAME
	Allowed []string // policy in effect (default or override)
}

// Error implements error.
func (e *EventContextError) Error() string {
	return fmt.Sprintf("trigger event %q is not allowed (allowed: %s)", e.Got, strings.Join(e.Allowed, ", "))
}

// RequireAllowedEvent validates that eventName is in allowed. Empty
// `allowed` falls back to DefaultAllowedEvents — callers should pass nil
// to use the default rather than constructing the slice themselves.
//
// Returns a *EventContextError on refuse so the app layer can render
// trigger-specific guidance ("PR-context callers expose secrets to the
// PR HEAD; gate the caller workflow…").
func RequireAllowedEvent(eventName string, allowed []string) error {
	if strings.TrimSpace(eventName) == "" {
		return fmt.Errorf("GITHUB_EVENT_NAME is empty: %w", errs.ErrMissingInput)
	}

	if len(allowed) == 0 {
		allowed = DefaultAllowedEvents
	}

	for _, ok := range allowed {
		if ok == eventName {
			return nil
		}
	}

	// Copy + sort the allowlist for deterministic error output. Adopters
	// reading the message in a workflow log get the same ordering
	// regardless of how the caller built the slice.
	sorted := append([]string(nil), allowed...)
	sort.Strings(sorted)

	return &EventContextError{Got: eventName, Allowed: sorted}
}

// IsPullRequestEvent reports whether the GHA event-name belongs to the
// PR family. The app layer uses this to render PR-specific guidance
// (since most refusals will be PR misconfigs).
func IsPullRequestEvent(eventName string) bool {
	switch eventName {
	case "pull_request",
		"pull_request_target",
		"pull_request_review",
		"pull_request_review_comment",
		"issue_comment":
		return true
	}

	return false
}
