// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package secretenv_test

import (
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/secretenv"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// TestNames_CoverEveryCredentialTheBinaryResolves is the check a hand-written
// list cannot make of itself: a credential name added to runcontext, or read
// by a command as a secret, and never added here would leave a subprocess
// holding it. runcontext is the authority for forge tokens; the command-level
// secrets are named explicitly.
func TestNames_CoverEveryCredentialTheBinaryResolves(t *testing.T) {
	t.Parallel()

	names := secretenv.Names()

	var missing []string

	for _, key := range slices.Concat(runcontext.Token().Keys(), runcontext.ReleaseToken().Keys(),
		[]string{"SSH_SIGNING_KEY", "GPG_PRIVATE_KEY", "GPG_PASSPHRASE", "COSIGN_PRIVATE_KEY", "COSIGN_PASSWORD", "REGISTRY_PASSWORD", "REGISTRY_TOKEN"}) {
		if !slices.Contains(names, key) {
			missing = append(missing, key)
		}
	}

	if len(missing) > 0 {
		t.Errorf("the binary reads %v as credentials, but secretenv.Names does not scrub them", missing)
	}

	if !slices.IsSorted(names) || len(slices.Compact(slices.Clone(names))) != len(names) {
		t.Errorf("Names must stay sorted and unique so additions are reviewed in one place: %v", names)
	}
}
