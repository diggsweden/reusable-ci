// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate_test

import (
	"reflect"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/validate"
)

func TestDecideAuthorization_SnapshotBypass(t *testing.T) {
	t.Parallel()
	d := validate.DecideAuthorization("v1.0.0-SNAPSHOT", "alice", "bob,charlie")
	if d.Outcome != validate.AuthorizationSnapshotBypass {
		t.Errorf("outcome = %q", d.Outcome)
	}
	if len(d.AuthorizedUsers) != 0 {
		t.Errorf("snapshot path shouldn't populate AuthorizedUsers, got %v", d.AuthorizedUsers)
	}
}

func TestDecideAuthorization_NoRestrictions(t *testing.T) {
	t.Parallel()
	for _, devs := range []string{"", "   ", "\n  \t"} {
		d := validate.DecideAuthorization("v1.0.0", "alice", devs)
		if d.Outcome != validate.AuthorizationNoRestrictions {
			t.Errorf("authorizedDevs=%q outcome = %q", devs, d.Outcome)
		}
	}
}

func TestDecideAuthorization_Allowed(t *testing.T) {
	t.Parallel()
	d := validate.DecideAuthorization("v1.0.0", "bob", "alice,bob,charlie")
	if d.Outcome != validate.AuthorizationAllowed {
		t.Errorf("outcome = %q", d.Outcome)
	}
	want := []string{"alice", "bob", "charlie"}
	if !reflect.DeepEqual(d.AuthorizedUsers, want) {
		t.Errorf("AuthorizedUsers = %v, want %v", d.AuthorizedUsers, want)
	}
}

func TestDecideAuthorization_AllowedTrimsWhitespace(t *testing.T) {
	t.Parallel()
	// CSV with spaces around entries — trim is applied.
	d := validate.DecideAuthorization("v1.0.0", "bob", "alice, bob ,charlie ")
	if d.Outcome != validate.AuthorizationAllowed {
		t.Errorf("outcome = %q (whitespace-trimmed entries should match)", d.Outcome)
	}
}

func TestDecideAuthorization_Denied(t *testing.T) {
	t.Parallel()
	d := validate.DecideAuthorization("v1.0.0", "eve", "alice,bob")
	if d.Outcome != validate.AuthorizationDenied {
		t.Errorf("outcome = %q", d.Outcome)
	}
	if !reflect.DeepEqual(d.AuthorizedUsers, []string{"alice", "bob"}) {
		t.Errorf("AuthorizedUsers = %v", d.AuthorizedUsers)
	}
}
