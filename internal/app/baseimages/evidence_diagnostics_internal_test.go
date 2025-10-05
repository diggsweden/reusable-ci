// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package baseimages

import (
	"bytes"
	"testing"
)

// TestPrintBaseImageVerificationError_DropsCredentialBearingCosignLines: the
// cosign stderr relayed on a failed verification can quote registry
// credentials. The diagnostic keeps the flavor, digest, failed check and every
// ordinary cosign line in order, drops each line carrying a credential marker,
// a credential URL or a JWT, and drops blank lines.
func TestPrintBaseImageVerificationError_DropsCredentialBearingCosignLines(t *testing.T) {
	t.Parallel()

	const jwt = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJjaSJ9.c2lnbmF0dXJlLWJ5dGVz"

	stderr := "Error: no matching attestations\n" +
		"GET https://registry.example/v2/: Authorization: Bearer abc123\n" +
		"\n" +
		"fetching https://user:hunter2@registry.example/v2/x\n" +
		"using token " + jwt + "\n" +
		"session " + jwt + "\n" +
		"  main.go:12: verification failed  \n"

	var out bytes.Buffer

	printBaseImageVerificationError(&out, "rust", "sha256:abc", "signature", []byte(stderr))

	want := "  flavor: rust\n  digest: sha256:abc\n  check:  signature\n" +
		"  cosign: Error: no matching attestations\n" +
		"  cosign: main.go:12: verification failed\n"
	if out.String() != want {
		t.Errorf("diagnostic = %q, want %q", out.String(), want)
	}
}
