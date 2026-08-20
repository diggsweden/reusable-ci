// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cosign_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

// PublicKey was uncovered. It exists so a signer-boundary command can
// derive the verification key from the private signing material without
// that key travelling through workflow shell -- so the public key has to
// arrive on the caller's writer, and nothing about the private key may
// arrive anywhere else.
//
// No t.Parallel(): mockbinary prepends to PATH via t.Setenv.

const testPublicKey = "-----BEGIN PUBLIC KEY-----\nMFkw\n-----END PUBLIC KEY-----\n"

func TestPublicKey_WritesTheKeyToTheCallersWriter(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", `printf '%s' '`+testPublicKey+`'`)

	var out, errOut bytes.Buffer

	adapter := &cosign.Adapter{Bin: bins.Path("cosign")}
	if err := adapter.PublicKey(context.Background(), "cosign.key", &out, &errOut); err != nil {
		t.Fatalf("PublicKey: %v", err)
	}

	if out.String() != testPublicKey {
		t.Errorf("stdout = %q, want the public key verbatim", out.String())
	}

	invs := bins.Invocations("cosign")
	if len(invs) != 1 {
		t.Fatalf("cosign invocations = %d, want 1", len(invs))
	}

	want := []string{"public-key", "--key", "cosign.key"}
	if !slices.Equal(invs[0].Args, want) {
		t.Errorf("argv = %v, want %v", invs[0].Args, want)
	}
}

// TestPublicKey_StderrIsRedacted covers why stderr goes through the
// redactor rather than straight to the caller: this command is pointed
// at private key material, and a cosign that echoed part of it on
// failure would put it in the CI log.
//
// The positive control uses a marker RedactKeyMaterial knows. Its
// sibling below is the one that matters.
func TestPublicKey_StderrIsRedacted(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", `printf 'error: -----BEGIN EC PRIVATE KEY-----\nsecretmaterial\n-----END EC PRIVATE KEY-----\n' >&2; exit 1`)

	var out, errOut bytes.Buffer

	adapter := &cosign.Adapter{Bin: bins.Path("cosign")}

	err := adapter.PublicKey(context.Background(), "cosign.key", &out, &errOut)
	if err == nil {
		t.Fatal("a failing cosign was reported as success")
	}

	if strings.Contains(errOut.String(), "secretmaterial") {
		t.Errorf("private key material reached the caller's stderr:\n%s", errOut.String())
	}
}

// TestPublicKey_CosignsOwnKeyFormatIsRedacted covers the format this
// project's own signing material is stored in. A key from cosign 3.x
// opens "-----BEGIN ENCRYPTED SIGSTORE PRIVATE KEY-----", which matches
// none of the redactor's original six PEM markers -- "BEGIN ENCRYPTED
// PRIVATE KEY" only if you skip the word in the middle. It was the one
// private-key shape the redactor could not see, in the adapter that
// exists to run cosign.
//
// The header used here is the real one, taken from a generated key
// rather than from memory.
func TestPublicKey_CosignsOwnKeyFormatIsRedacted(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", `printf 'error: -----BEGIN ENCRYPTED SIGSTORE PRIVATE KEY-----\nsecretmaterial\n-----END ENCRYPTED SIGSTORE PRIVATE KEY-----\n' >&2; exit 1`)

	var out, errOut bytes.Buffer

	adapter := &cosign.Adapter{Bin: bins.Path("cosign")}

	if err := adapter.PublicKey(context.Background(), "cosign.key", &out, &errOut); err == nil {
		t.Fatal("a failing cosign was reported as success")
	}

	if strings.Contains(errOut.String(), "secretmaterial") {
		t.Errorf("cosign private key material reached the caller's stderr:\n%s", errOut.String())
	}
}

// TestPublicKey_RejectsAnEmptyKeyRef keeps `cosign public-key --key ""`
// from running, which resolves against cosign's own defaults rather than
// failing, and could emit a key the caller never asked for.
func TestPublicKey_RejectsAnEmptyKeyRef(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", `printf 'should not run' `)

	var out bytes.Buffer

	adapter := &cosign.Adapter{Bin: bins.Path("cosign")}

	if err := adapter.PublicKey(context.Background(), "", &out, nil); !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	if got := len(bins.Invocations("cosign")); got != 0 {
		t.Errorf("cosign ran %d times despite the refusal", got)
	}

	if out.Len() != 0 {
		t.Errorf("something was written to the caller's writer: %q", out.String())
	}
}

// TestPublicKey_FailureWritesNoKey keeps a failed derivation from
// looking like a successful one to a caller that only checks the writer.
func TestPublicKey_FailureWritesNoKey(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", `printf 'no such key\n' >&2; exit 2`)

	var out, errOut bytes.Buffer

	adapter := &cosign.Adapter{Bin: bins.Path("cosign")}

	if err := adapter.PublicKey(context.Background(), "missing.key", &out, &errOut); err == nil {
		t.Fatal("a missing key was reported as success")
	}

	if out.Len() != 0 {
		t.Errorf("stdout carried %q after a failure", out.String())
	}

	// The operator still needs cosign's own diagnosis.
	if !strings.Contains(errOut.String(), "no such key") {
		t.Errorf("cosign's diagnostic did not reach the caller: %q", errOut.String())
	}
}
