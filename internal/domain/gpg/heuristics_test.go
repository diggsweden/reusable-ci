// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gpg_test

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/gpg"
)

func TestIsArmored_IgnoresLeadingWhitespaceBeforeTheHeader(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"-----BEGIN PGP PRIVATE KEY BLOCK-----\n…":   true,
		"\n-----BEGIN PGP PRIVATE KEY BLOCK-----\n…": true,
		"\n\n-----BEGIN something":                   true,
		// A secret authored on Windows, and one indented by a YAML block
		// scalar. Both used to fall through to the base64 branch and fail
		// with "illegal base64 data" about a perfectly good armored key.
		"\r\n-----BEGIN PGP PRIVATE KEY BLOCK-----\r\n…": true,
		"  -----BEGIN PGP PRIVATE KEY BLOCK-----\n…":     true,
		"\t-----BEGIN PGP PRIVATE KEY BLOCK-----\n…":     true,
		"bGFsYWxh":     false, // base64 of "lalala"
		"":             false,
		"random bytes": false,
	}
	for in, want := range tests {
		t.Run(in[:min(len(in), 20)], func(t *testing.T) {
			t.Parallel()

			if got := gpg.IsArmored(in); got != want {
				t.Errorf("IsArmored(%q) = %v, want %v", in, got, want)
			}
		})
	}
}

func TestDecodeKey_Armored(t *testing.T) {
	t.Parallel()

	armored := "-----BEGIN PGP PRIVATE KEY BLOCK-----\nstuff\n-----END PGP PRIVATE KEY BLOCK-----" //nolint:gosec // synthetic placeholder; not a real key.

	out, err := gpg.DecodeKey(armored)
	if err != nil {
		t.Fatal(err)
	}

	if string(out) != armored {
		t.Errorf("armored input was modified")
	}
}

func TestDecodeKey_Base64(t *testing.T) {
	t.Parallel()

	original := "-----BEGIN PGP PRIVATE KEY BLOCK-----\nactual key body\n-----END PGP PRIVATE KEY BLOCK-----" //nolint:gosec // synthetic placeholder; not a real key.
	encoded := base64.StdEncoding.EncodeToString([]byte(original))

	out, err := gpg.DecodeKey(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if string(out) != original {
		t.Errorf("decode mismatch: got %q, want %q", out, original)
	}
}

func TestDecodeKey_Base64WithWhitespace(t *testing.T) {
	t.Parallel()

	original := "key payload"
	encoded := base64.StdEncoding.EncodeToString([]byte(original))
	// Wrap to lines of 4 to simulate a multi-line secret in YAML.
	var wrapped strings.Builder

	for i := 0; i < len(encoded); i += 4 { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		wrapped.WriteString(encoded[i:min(i+4, len(encoded))])
		wrapped.WriteString("\n")
	}

	out, err := gpg.DecodeKey(wrapped.String())
	if err != nil {
		t.Fatal(err)
	}

	if string(out) != original {
		t.Errorf("decode mismatch with whitespace: got %q, want %q", out, original)
	}
}

func TestDecodeKey_InvalidBase64Errors(t *testing.T) {
	t.Parallel()

	_, err := gpg.DecodeKey("not valid base64 ===///===")
	// A secret that is neither armored nor base64 is malformed input (65),
	// not an internal bug: the operator pasted the wrong thing.
	if !errors.Is(err, errs.ErrMalformedInput) {
		t.Fatalf("err = %v, want ErrMalformedInput", err)
	}

	// And the message must not echo the value back -- it is a secret.
	if strings.Contains(err.Error(), "not valid base64 ===///===") {
		t.Errorf("the rejection echoed the supplied secret: %v", err)
	}
}

func TestHexEncodePassphrase_EncodesBytesAsUppercaseHex(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"":            "",
		"abc":         "616263",
		"ABC":         "414243",
		"\x00\xff":    "00FF",
		"hello world": "68656C6C6F20776F726C64",
		// Unicode passes through byte-by-byte (UTF-8 encoded source).
		// "ä" = 0xC3 0xA4
		"ä": "C3A4",
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			t.Parallel()

			if got := gpg.HexEncodePassphrase(in); got != want {
				t.Errorf("HexEncodePassphrase(%q) = %q, want %q", in, got, want)
			}
		})
	}
}

func TestAgentConfig_HasExpectedDirectives(t *testing.T) {
	t.Parallel()

	for _, directive := range []string{
		"default-cache-ttl 21600",
		"max-cache-ttl 31536000",
		"allow-preset-passphrase",
	} {
		// gocritic's argOrder heuristic flags this as "looks reversed"
		// because gpg.AgentConfig is the named constant while directive
		// is the loop var — but semantically gpg.AgentConfig IS the
		// haystack and directive IS the needle.
		//nolint:gocritic // argOrder false positive — see comment above
		if !strings.Contains(gpg.AgentConfig, directive) {
			t.Errorf("AgentConfig missing directive %q", directive)
		}
	}

	// The file must carry a delineating marker so a reader can see
	// reusable-ci wrote it (clig.dev §Configuration), and the marker must
	// be a gpg-agent.conf comment so it doesn't change agent behaviour.
	if !strings.HasPrefix(gpg.AgentConfig, "# Managed by reusable-ci") {
		t.Errorf("AgentConfig must begin with a '# Managed by reusable-ci' marker comment; got:\n%s", gpg.AgentConfig)
	}
}

// TestDecodeKey_Base64WrappedWithCRLFOrTabs covers the wrappings a secret
// picks up on its way through a Windows editor or an indented YAML block.
// Every whitespace kind is stripped before decoding, so all spellings yield
// the same key bytes.
func TestDecodeKey_Base64WrappedWithCRLFOrTabs(t *testing.T) {
	t.Parallel()

	const armored = "-----BEGIN PGP PRIVATE KEY BLOCK-----\nbody\n-----END PGP PRIVATE KEY BLOCK-----" //nolint:gosec // armor framing around a placeholder body, no key material

	encoded := base64.StdEncoding.EncodeToString([]byte(armored))
	half := len(encoded) / 2

	for name, wrapped := range map[string]string{
		"crlf": encoded[:half] + "\r\n" + encoded[half:] + "\r\n",
		"tabs": "\t" + encoded[:half] + "\t\n\t" + encoded[half:] + "\n",
	} {
		got, err := gpg.DecodeKey(wrapped)
		if err != nil || string(got) != armored {
			t.Errorf("%s: DecodeKey = %q, %v; want the armored key", name, got, err)
		}
	}
}
