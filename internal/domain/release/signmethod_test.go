// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/release"
)

func TestParseSignMethod_Accepts(t *testing.T) {
	for _, name := range []string{"gpg", "sigstore", "kms"} {
		got, err := release.ParseSignMethod(name)
		if err != nil {
			t.Errorf("ParseSignMethod(%q) error: %v", name, err)
		}

		if string(got) != name {
			t.Errorf("ParseSignMethod(%q) = %q, want %q", name, got, name)
		}
	}
}

func TestParseSignMethod_EmptyIsMissingInput(t *testing.T) {
	_, err := release.ParseSignMethod("")
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("empty input: expected ErrMissingInput, got %v", err)
	}
}

func TestParseSignMethod_UnknownIsInvalidConfig(t *testing.T) {
	_, err := release.ParseSignMethod("openssl")
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Errorf("unknown method: expected ErrInvalidConfig, got %v", err)
	}
}

func TestSignatureExtensions(t *testing.T) {
	cases := []struct {
		method release.SignMethod
		want   []string
	}{
		{release.SignMethodGPG, []string{".asc"}},
		{release.SignMethodSigstore, []string{".bundle"}},
		{release.SignMethodKMS, []string{".bundle"}},
	}

	for _, c := range cases {
		got := c.method.SignatureExtensions()
		if !slices.Equal(got, c.want) {
			t.Errorf("%s.SignatureExtensions() = %v, want %v", c.method, got, c.want)
		}
	}
}

func TestDefaultSignMethod_IsGPG(t *testing.T) {
	// Pinned: changing the default is a contract break for every
	// existing consumer who relies on .asc verification. If we move
	// the default, do it in a major version bump.
	if release.DefaultSignMethod != release.SignMethodGPG {
		t.Errorf("DefaultSignMethod = %q, want gpg (changing this breaks downstream consumers)", release.DefaultSignMethod)
	}
}
