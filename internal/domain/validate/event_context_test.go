// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/validate"
)

const (
	eventPush = "push"
	eventPR   = "pull_request"
)

func TestRequireAllowedEvent_RejectsEventsOutsideTheAllowlist(t *testing.T) {
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

func TestIsPullRequestEvent_RecognisesTheWholePullRequestFamily(t *testing.T) {
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

// TestRequireAllowedEvent_AnOverrideReplacesTheDefaults pins what a custom
// allowlist means. It is the whole policy, not an addition to the default one:
// a step that allows pull_request for a preview deploy must not also accept
// push unless it says so. Matching is exact -- no trimming, no case folding --
// because event names come from the forge in one spelling, and the CLI trims
// the list before it gets here. The diagnostic lists the policy sorted, from a
// copy, so the caller's slice is left as it was.
func TestRequireAllowedEvent_AnOverrideReplacesTheDefaults(t *testing.T) {
	t.Parallel()

	refused := func(t *testing.T, err error) *validate.EventContextError {
		t.Helper()

		var ece *validate.EventContextError
		if !errors.As(err, &ece) {
			t.Fatalf("err = %v, want *EventContextError", err)
		}

		return ece
	}

	t.Run("a later entry is matched", func(t *testing.T) {
		t.Parallel()

		if err := validate.RequireAllowedEvent(eventPR, []string{"deployment", "workflow_call", eventPR}); err != nil {
			t.Errorf("err = %v, want the third entry to allow it", err)
		}
	})

	t.Run("a default event outside the override is refused", func(t *testing.T) {
		t.Parallel()

		allowed := []string{"zeta_event", eventPR, "alpha_event"}
		ece := refused(t, validate.RequireAllowedEvent(eventPush, allowed))

		if want := []string{"alpha_event", eventPR, "zeta_event"}; !slices.Equal(ece.Allowed, want) {
			t.Errorf("Allowed = %v, want only the override, sorted: %v", ece.Allowed, want)
		}

		if want := []string{"zeta_event", eventPR, "alpha_event"}; !slices.Equal(allowed, want) {
			t.Errorf("caller's allowlist became %v, want it unchanged", allowed)
		}
	})

	for name, event := range map[string]string{
		"case differs":       "Push",
		"leading whitespace": " push",
		"trailing newline":   "push\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if ece := refused(t, validate.RequireAllowedEvent(event, nil)); ece.Got != event {
				t.Errorf("Got = %q, want the event as given", ece.Got)
			}
		})
	}

	t.Run("an allowlist entry with whitespace matches nothing", func(t *testing.T) {
		t.Parallel()

		if ece := refused(t, validate.RequireAllowedEvent(eventPush, []string{" push "})); ece.Got != eventPush {
			t.Errorf("Got = %q, want %q", ece.Got, eventPush)
		}
	})
}
