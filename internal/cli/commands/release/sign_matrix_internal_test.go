// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSignFlagMatrix_AllMethodCells(t *testing.T) {
	t.Parallel()

	for _, method := range []domainrelease.SignMethod{domainrelease.SignMethodGPG, domainrelease.SignMethodSigstore, domainrelease.SignMethodKMS} {
		for _, field := range []string{"none", "key", "issuer", "private", "passphrase"} {
			key, issuer, private, pass := "", "", "", ""
			if method == domainrelease.SignMethodKMS {
				key = "fixture"
			}

			switch field {
			case "key":
				key = "fixture"
			case "issuer":
				issuer = "fixture"
			case "private":
				private = "fixture"
			case "passphrase":
				pass = "fixture"
			}

			err := validateSignFlags(method, key, issuer, private, pass)

			forbidden := (method == domainrelease.SignMethodGPG && (field == "key" || field == "issuer")) || (method == domainrelease.SignMethodSigstore && (field == "key" || field == "private" || field == "passphrase")) || (method == domainrelease.SignMethodKMS && (field == "issuer" || field == "private" || field == "passphrase"))
			if forbidden {
				require.ErrorIs(t, err, errs.ErrInvalidConfig)
			} else {
				require.NoError(t, err)
			}
		}
	}

	err := validateSignFlags(domainrelease.SignMethodKMS, "", "", "", "")
	require.ErrorIs(t, err, errs.ErrInvalidConfig)
	require.Contains(t, err.Error(), "--key is required")
}
