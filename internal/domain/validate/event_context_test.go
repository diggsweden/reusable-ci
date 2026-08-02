// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/validate"
)

const (
	eventPush = "push"
	eventPR   = "pull_request"
)

func TestRequireAllowedEvent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		event     string
		allowed   []string
		wantErr   bool
		wantIsPR  bool
		wantMatch string
	}{
		{name: "push allowed by default", event: eventPush},
		{name: "workflow_dispatch allowed by default", event: "workflow_dispatch"},
		{name: "release allowed by default", event: "release"},
		{name: "schedule allowed by default", event: "schedule"},
		{name: "workflow_run allowed by default", event: "workflow_run"},
		{name: "merge_group allowed by default", event: "merge_group"},

		{name: "pull_request refused", event: eventPR, wantErr: true, wantIsPR: true, wantMatch: eventPR},
		{name: "pull_request_target refused", event: "pull_request_target", wantErr: true, wantIsPR: true},
		{name: "pull_request_review refused", event: "pull_request_review", wantErr: true, wantIsPR: true},
		{name: "pull_request_review_comment refused", event: "pull_request_review_comment", wantErr: true, wantIsPR: true},
		{name: "issue_comment refused", event: "issue_comment", wantErr: true, wantIsPR: true},

		{name: "unknown event refused", event: "fork", wantErr: true},

		{name: "custom allowlist accepts pull_request", event: eventPR, allowed: []string{eventPR}},
		{name: "custom allowlist refuses push", event: eventPush, allowed: []string{eventPR}, wantErr: true},

		{name: "empty input fails", event: "", wantErr: true, wantMatch: "GITHUB_EVENT_NAME"},
		{name: "whitespace-only fails", event: "  ", wantErr: true, wantMatch: "GITHUB_EVENT_NAME"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validate.RequireAllowedEvent(tc.event, tc.allowed)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for event %q", tc.event)
				}

				if tc.wantMatch != "" && !strings.Contains(err.Error(), tc.wantMatch) {
					t.Errorf("error missing %q: %v", tc.wantMatch, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestRequireAllowedEvent_EmptyInputWrapsMissingInput(t *testing.T) {
	t.Parallel()

	err := validate.RequireAllowedEvent("", nil)
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("expected ErrMissingInput, got %v", err)
	}
}

func TestRequireAllowedEvent_RefuseReturnsStructuredError(t *testing.T) {
	t.Parallel()

	err := validate.RequireAllowedEvent(eventPR, nil)

	var ece *validate.EventContextError
	if !errors.As(err, &ece) {
		t.Fatalf("expected *EventContextError, got %T: %v", err, err)
	}

	if ece.Got != eventPR {
		t.Errorf("Got = %q, want %q", ece.Got, eventPR)
	}

	// Sorted-allowlist invariant: the structured error must list the
	// allowed events in stable order so adopters see consistent messages.
	if !sortedAscending(ece.Allowed) {
		t.Errorf("Allowed not sorted: %v", ece.Allowed)
	}
}

func TestIsPullRequestEvent(t *testing.T) {
	t.Parallel()

	prFamily := []string{
		"pull_request",
		"pull_request_target",
		"pull_request_review",
		"pull_request_review_comment",
		"merge_request_event", // GitLab raw spelling via explicit --event-name
		"issue_comment",
	}
	for _, e := range prFamily {
		if !validate.IsPullRequestEvent(e) {
			t.Errorf("IsPullRequestEvent(%q) = false, want true", e)
		}
	}

	for _, e := range []string{"push", "release", "workflow_dispatch", "", "fork"} {
		if validate.IsPullRequestEvent(e) {
			t.Errorf("IsPullRequestEvent(%q) = true, want false", e)
		}
	}
}

func sortedAscending(in []string) bool {
	for i := 1; i < len(in); i++ {
		if in[i-1] > in[i] {
			return false
		}
	}

	return true
}
