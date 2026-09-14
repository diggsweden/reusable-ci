// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
)

func TestEventContext_AllowedPrintsConfirmation(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	if err := appvalidate.EventContext(&out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.EventContextInput{EventName: "push"}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "push") {
		t.Errorf("confirmation missing event name: %q", out.String())
	}
}

func TestEventContext_PullRequestRefusedWithGuidance(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := appvalidate.EventContext(&out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.EventContextInput{EventName: "pull_request_target"})
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

	err := appvalidate.EventContext(&out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.EventContextInput{EventName: "fork"})
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
	if err := appvalidate.EventContext(&out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.EventContextInput{
		EventName:     "pull_request",
		AllowedEvents: []string{"pull_request"},
	}); err != nil {
		t.Errorf("custom allowlist should permit pull_request: %v", err)
	}
}

func TestEventContext_EmptyEventNameWrapsMissingInput(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := appvalidate.EventContext(&out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.EventContextInput{EventName: ""})
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("expected ErrMissingInput, got %v", err)
	}
}

// TestEventContext_HostileEventNameStaysOneAnnotation feeds an event name that
// carries a newline and a workflow command. The name comes from the workflow
// context, so a refusal must not let it open a second annotation or end the
// first early; the Go error quotes it instead of printing it raw.
func TestEventContext_HostileEventNameStaysOneAnnotation(t *testing.T) {
	t.Parallel()

	const hostile = "fork\n::warning title=owned::injected"

	var out bytes.Buffer

	err := appvalidate.EventContext(&out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.EventContextInput{EventName: hostile})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	annotation := out.String()
	if strings.Count(annotation, "\n") != 1 || !strings.HasPrefix(annotation, "::error title=Refused trigger event::") || strings.Contains(annotation, "\n::warning") {
		t.Errorf("annotation = %q, want a single ::error line", annotation)
	}

	if strings.Contains(err.Error(), hostile) {
		t.Errorf("error prints the event name raw: %q", err.Error())
	}

	// The generic guidance must say an override replaces the policy: the old
	// "extend" wording led adopters to list only the event they were adding.
	if !strings.Contains(err.Error(), "replaces the default policy") {
		t.Errorf("guidance does not say the override replaces the defaults: %v", err)
	}
}
