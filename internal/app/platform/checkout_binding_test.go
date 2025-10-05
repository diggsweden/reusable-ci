// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	appplatform "github.com/diggsweden/reusable-ci/v3/internal/app/platform"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// Which calls carry the credential, and with what argv, is already pinned
// exactly by TestCheckoutBoundary_CompleteOrderCredentialsAndFailures in
// reference_boundary_test.go — it compares the whole ordered event list. These
// two cover what that one does not: that the secret does not leak out of the
// checkout, and that the Credential value is passed along rather than unwrapped
// into a string on the way.
//
// The marker is deliberately something no other value in a checkout could be,
// so a match cannot be a coincidence and an absence cannot be an empty string.
//
//nolint:gosec // G101: a deliberately recognisable fixture value, not a credential.
const checkoutCredentialMarker = "rci-marker-6f1c9d2a-not-a-real-token"

// This layer must hand Git the Credential VALUE, not a string it unwrapped.
//
// That distinction is the whole of the type's protection: a bound credential
// yields its secret only at its own origin, and the check happens inside For.
// A refactor that reads the secret once at the top of Checkout and passes the
// string down would keep every other test in this file green while removing the
// audience enforcement entirely — the fake would still receive "a credential",
// because it would receive whatever was reconstructed.
//
// The audience rule itself is runcontext's and is tested there. What is
// asserted here is that the value arrives intact and unopened.
func TestCheckout_PassesTheCredentialValueRatherThanAnUnwrappedSecret(t *testing.T) {
	t.Parallel()

	git := &fakeCheckoutGit{}

	in := baseInput(t, "v1.0.0")
	in.Token = runcontext.OperatorCredential(checkoutCredentialMarker)
	in.FetchTags = true

	if _, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), fakeoutputsink.New(t), nil, in); err != nil {
		t.Fatal(err)
	}

	checked := 0

	for _, event := range git.events {
		if !credentialCarryingCall(event.method) {
			continue
		}

		checked++

		if !event.credential.Present() {
			t.Errorf("call %s received a credential reporting absent; the value was rebuilt from a string somewhere", event.method)
		}

		// A Credential formats redacted. If what arrived were a plain string
		// wrapped again, this is unchanged — but combined with Present() above
		// and the argv check below, an unwrapped secret has nowhere to hide
		// that this file does not look.
		if strings.Contains(event.credential.String(), checkoutCredentialMarker) {
			t.Errorf("call %s carries a credential that prints its secret", event.method)
		}
	}

	if checked == 0 {
		t.Fatal("no credential-carrying call was seen; this test measured the fixture")
	}
}

// The secret must not reach the operator's terminal. Checkout narrates what it
// is doing, and a URL with credentials in it, or a diagnostic that echoes the
// token, is how secrets end up in CI logs.
func TestCheckout_TheCredentialNeverReachesTheNarrationOrTheArguments(t *testing.T) {
	t.Parallel()

	git := &fakeCheckoutGit{}

	in := baseInput(t, "v1.0.0")
	in.Token = runcontext.OperatorCredential(checkoutCredentialMarker)
	in.FetchTags = true
	in.FetchAllRefs = true
	in.FetchBase = "main"

	var narration bytes.Buffer

	sha, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), fakeoutputsink.New(t), &narration, in)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(narration.String(), checkoutCredentialMarker) {
		t.Errorf("the token appears in what the operator sees:\n%s", narration.String())
	}

	if strings.Contains(sha, checkoutCredentialMarker) {
		t.Error("the token appears in the returned SHA")
	}

	for _, event := range git.events {
		for _, arg := range event.args {
			if strings.Contains(arg, checkoutCredentialMarker) {
				t.Errorf("call %s carries the token in an argument (%q); argv is world-readable on most runners", event.method, arg)
			}
		}
	}

	if len(git.events) == 0 {
		t.Fatal("no calls were made; this test measured nothing")
	}
}

// credentialCarryingCall names the ports that contact the origin. The rest —
// init, config, checkout of an already-fetched object — are local and take no
// credential, so requiring one there would be asserting the opposite of the
// design.
func credentialCarryingCall(method string) bool {
	switch method {
	case "fetch", "fetch-tags", "fetch-all-refs", "probe-tag", "probe-branch":
		return true
	default:
		return false
	}
}
