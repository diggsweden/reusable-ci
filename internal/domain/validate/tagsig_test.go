// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate_test

import (
	"reflect"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/validate"
)

func TestDetectTagSignatures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want validate.SignaturePresence
	}{
		{"none", "object 1234\ntype commit\ntag v1\n\nplain message", validate.SignaturePresence{}},
		{"gpg", "object 1234\n-----BEGIN PGP SIGNATURE-----\n...\n-----END PGP SIGNATURE-----", validate.SignaturePresence{HasGPG: true}},
		{"ssh", "tag body\n-----BEGIN SSH SIGNATURE-----\nblob\n-----END SSH SIGNATURE-----", validate.SignaturePresence{HasSSH: true}},
		{"both", "BEGIN PGP SIGNATURE\nBEGIN SSH SIGNATURE", validate.SignaturePresence{HasGPG: true, HasSSH: true}},
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

func TestParseGoodSignerFromVerify(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{
			"gpg: Signature made Mon 12 Feb 2024 11:22:33\ngpg: Good signature from \"Test Bot <bot@example.invalid>\" [unknown]\n",
			"Test Bot <bot@example.invalid>",
		},
		{"no good line", ""},
	}
	for _, c := range cases {
		got := validate.ParseGoodSignerFromVerify(c.in)
		if got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

func TestFilterOutTag(t *testing.T) {
	t.Parallel()
	got := validate.FilterOutTag([]string{"v1.0.0", "v1.0.0-alias", "v2.0.0"}, "v1.0.0")
	want := []string{"v1.0.0-alias", "v2.0.0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
