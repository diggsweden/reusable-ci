// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/internal/app/validate"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

func TestEventContext_AllowedPrintsConfirmation(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	if err := appvalidate.EventContext(&out, appvalidate.EventContextInput{EventName: "push"}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "push") {
		t.Errorf("confirmation missing event name: %q", out.String())
	}
}

func TestEventContext_PullRequestRefusedWithGuidance(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := appvalidate.EventContext(&out, appvalidate.EventContextInput{EventName: "pull_request_target"})
	if err == nil {
		t.Fatal("expected refusal")
	}

	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}

	// Workflow-command annotation on stdout so the GHA run-summary
	// surfaces the refusal cleanly.
	if !strings.HasPrefix(out.String(), "::error") {
		t.Errorf("missing ::error annotation: %q", out.String())
	}

	// PR-specific guidance must mention --allowed-events so the adopter
	// has a one-line escape hatch for explicit preview flows.
	if !strings.Contains(err.Error(), "--allowed-events") {
		t.Errorf("missing --allowed-events guidance: %v", err)
	}
}

func TestEventContext_UnknownEventGetsGenericGuidance(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := appvalidate.EventContext(&out, appvalidate.EventContextInput{EventName: "fork"})
	if err == nil {
		t.Fatal("expected refusal")
	}

	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}

	if strings.Contains(err.Error(), "PR-context") {
		t.Errorf("non-PR refusal should not carry PR-specific guidance: %v", err)
	}
}

func TestEventContext_CustomAllowlistAcceptsPullRequest(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	if err := appvalidate.EventContext(&out, appvalidate.EventContextInput{
		EventName:     "pull_request",
		AllowedEvents: []string{"pull_request"},
	}); err != nil {
		t.Errorf("custom allowlist should permit pull_request: %v", err)
	}
}

func TestEventContext_EmptyEventNameWrapsMissingInput(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := appvalidate.EventContext(&out, appvalidate.EventContextInput{EventName: ""})
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("expected ErrMissingInput, got %v", err)
	}
}
