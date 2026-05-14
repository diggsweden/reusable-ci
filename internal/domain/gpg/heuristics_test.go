// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package gpg_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/gpg"
)

func TestIsArmored(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"-----BEGIN PGP PRIVATE KEY BLOCK-----\n…":   true,
		"\n-----BEGIN PGP PRIVATE KEY BLOCK-----\n…": true,
		"\n\n-----BEGIN something":                   true,
		"bGFsYWxh":                                   false, // base64 of "lalala"
		"":                                           false,
		"random bytes":                               false,
	}
	for in, want := range tests {
		in, want := in, want
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
	armored := "-----BEGIN PGP PRIVATE KEY BLOCK-----\nstuff\n-----END PGP PRIVATE KEY BLOCK-----"
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
	original := "-----BEGIN PGP PRIVATE KEY BLOCK-----\nactual key body\n-----END PGP PRIVATE KEY BLOCK-----"
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
	for i := 0; i < len(encoded); i += 4 {
		end := i + 4
		if end > len(encoded) {
			end = len(encoded)
		}
		wrapped.WriteString(encoded[i:end])
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
	if err == nil {
		t.Error("expected error on invalid base64")
	}
}

func TestHexEncodePassphrase(t *testing.T) {
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
		in, want := in, want
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
	for _, want := range []string{
		"default-cache-ttl 21600",
		"max-cache-ttl 31536000",
		"allow-preset-passphrase",
	} {
		if !strings.Contains(gpg.AgentConfig, want) {
			t.Errorf("AgentConfig missing directive %q", want)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
