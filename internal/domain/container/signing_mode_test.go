// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestSigningModes_RejectKeylessFieldsForKMS(t *testing.T) {
	t.Parallel()

	ref := "registry.example/app@sha256:" + strings.Repeat("a", 64)
	for _, field := range []string{"issuer", "fulcio", "rekor", "root"} {
		sign := container.ImageSignRequest{ImageRef: ref, KeyRef: "kms-fixture"}

		switch field {
		case "issuer":
			sign.OIDCIssuer = "issuer"
		case "fulcio":
			sign.FulcioURL = "fulcio"
		case "rekor":
			sign.RekorURL = "rekor"
		case "root":
			sign.TrustedRootPath = "root"
		}

		require.ErrorIs(t, sign.Validate(), errs.ErrUsage)
		attest := container.ImageAttestRequest{ImageRef: ref, KeyRef: sign.KeyRef, OIDCIssuer: sign.OIDCIssuer, FulcioURL: sign.FulcioURL, RekorURL: sign.RekorURL, TrustedRootPath: sign.TrustedRootPath, PredicateType: "cyclonedx", PredicatePath: "fixture"}
		require.ErrorIs(t, attest.Validate(), errs.ErrUsage)

		sign.Keyless = true
		sign.KeyRef = ""
		attest.Keyless = true
		attest.KeyRef = ""

		require.NoError(t, sign.Validate())
		require.NoError(t, attest.Validate())
	}

	for _, identity := range []bool{false, true} {
		verify := container.ImageVerifyRequest{ImageRef: ref, KeyRef: "kms-fixture"}
		if identity {
			verify.CertIdentityRegexp = "identity"
		} else {
			verify.CertOIDCIssuer = "issuer"
		}

		require.ErrorIs(t, verify.Validate(), errs.ErrUsage)
		require.ErrorIs(t, (container.AttestationVerifyRequest{ImageRef: ref, KeyRef: verify.KeyRef, CertIdentityRegexp: verify.CertIdentityRegexp, CertOIDCIssuer: verify.CertOIDCIssuer, PredicateType: "cyclonedx"}).Validate(), errs.ErrUsage)
	}

	require.NoError(t, (container.ImageVerifyRequest{ImageRef: ref, KeyRef: "kms-fixture"}).Validate())
	require.NoError(t, (container.AttestationVerifyRequest{ImageRef: ref, KeyRef: "kms-fixture", PredicateType: "cyclonedx"}).Validate())
}
