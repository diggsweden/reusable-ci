// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/validate"
)

func TestDetectTagSignatures_DetectsGPGAndSSHSignatureBlocks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want validate.SignaturePresence
	}{
		{"none", "object 1234\ntype commit\ntag v1\n\nplain message", validate.SignaturePresence{}},
		{"gpg", "object 1234\n-----BEGIN PGP SIGNATURE-----\n...\n-----END PGP SIGNATURE-----", validate.SignaturePresence{HasGPG: true}},
		{"ssh", "tag body\n-----BEGIN SSH SIGNATURE-----\nblob\n-----END SSH SIGNATURE-----", validate.SignaturePresence{HasSSH: true}},
		{"both", "-----BEGIN PGP SIGNATURE-----\n-----BEGIN SSH SIGNATURE-----", validate.SignaturePresence{HasGPG: true, HasSSH: true}},
		// Git appends a signature as its own lines. The same words inside the
		// tag message are prose, and an unsigned tag that mentions them must
		// not pass the signed-tag gate.
		{"header mentioned in the message", "object 1234\ntype commit\ntag v1\ntagger dev\n\nverify the -----BEGIN PGP SIGNATURE----- block and the -----BEGIN SSH SIGNATURE----- docs\n", validate.SignaturePresence{}},
		{"header words without armor", "object 1234\n\nBEGIN PGP SIGNATURE\nBEGIN SSH SIGNATURE\n", validate.SignaturePresence{}},
	}
	for _, c := range cases {
		got := validate.DetectTagSignatures(c.body)
		if got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestSignaturePresence_Any(t *testing.T) {
	t.Parallel()

	if (validate.SignaturePresence{}).Any() {
		t.Error("empty presence should report Any=false")
	}

	if !(validate.SignaturePresence{HasGPG: true}).Any() {
		t.Error("HasGPG should report Any=true")
	}
}

func TestFilterOutTag_RemovesOnlyTheExactTag(t *testing.T) {
	t.Parallel()

	got := validate.FilterOutTag([]string{"v1.0.0", "v1.0.0-alias", "v2.0.0"}, "v1.0.0") //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.

	want := []string{"v1.0.0-alias", "v2.0.0"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
