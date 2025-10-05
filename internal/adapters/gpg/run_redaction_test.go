// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gpg_test

import (
	"context"
	"strings"
	"testing"

	adaptergpg "github.com/diggsweden/reusable-ci/v3/internal/adapters/gpg"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

// TestRun_FailureOutputIsRedacted covers the shared run path every gpg and
// gpg-connect-agent call goes through. A failing ListKeygrips or
// PresetPassphrase whose output echoes key material must reach the error,
// and so the CI log, with the body replaced rather than quoted.
func TestRun_FailureOutputIsRedacted(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("gpg", `printf -- '-----BEGIN PGP PRIVATE KEY BLOCK-----\nsecretbody\n'; exit 2`)
	bins.Add("gpg-connect-agent", `printf -- '-----BEGIN PGP PRIVATE KEY BLOCK-----\nsecretbody\n' >&2; exit 1`)

	adapter := &adaptergpg.Adapter{GPGBin: bins.Path("gpg"), AgentBin: bins.Path("gpg-connect-agent")}

	_, listErr := adapter.ListKeygrips(context.Background(), "ABCDEF0123456789ABCDEF0123456789ABCDEF01")
	presetErr := adapter.PresetPassphrase(context.Background(), "FEDCBA0987654321FEDCBA0987654321FEDCBA09", "pass")

	for name, err := range map[string]error{"ListKeygrips": listErr, "PresetPassphrase": presetErr} {
		if err == nil {
			t.Fatalf("%s: expected an error", name)
		}

		if strings.Contains(err.Error(), "secretbody") {
			t.Errorf("%s: key material reached the error: %v", name, err)
		}

		if !strings.Contains(err.Error(), "redacted") {
			t.Errorf("%s: error should say the output was redacted: %v", name, err)
		}
	}
}
