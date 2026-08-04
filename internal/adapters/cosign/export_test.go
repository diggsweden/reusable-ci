// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cosign

import "time"

// The signing-config builder is unexported because nothing outside this adapter
// should be composing cosign's documents. These aliases let the black-box test
// in this directory hold it to cosign's own output without widening the API.
type SigningConfigInputForTest = signingConfigInput

func BuildSigningConfigForTest(in SigningConfigInputForTest, validFrom time.Time) ([]byte, error) {
	return buildSigningConfig(in, validFrom)
}
