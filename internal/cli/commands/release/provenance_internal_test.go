// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"errors"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/signflags"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// TestProvenanceSignBlobInput_CarriesSigstoreEndpoints pins that the
// --fulcio-url / --rekor-url / --trusted-root flags `release provenance`
// declares reach cosign sign-blob. They were declared through
// signflags.Cosign and read nowhere, so a self-hosted Sigstore operator's
// provenance was silently signed against the public CA and log; for kms the
// CA and log overrides are refused, as the `release sign` signer already does.
func TestProvenanceSignBlobInput_CarriesSigstoreEndpoints(t *testing.T) {
	t.Parallel()

	endpoints := signflags.Endpoints{
		FulcioURL:       "https://fulcio.internal",
		RekorURL:        "https://rekor.internal",
		TrustedRootPath: "trusted_root.json",
	}

	got, err := provenanceSignBlobInput(signProvenanceInput{output: "p.json", method: "sigstore", endpoints: endpoints}, "p.json.bundle")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}

	if got.FulcioURL != endpoints.FulcioURL || got.RekorURL != endpoints.RekorURL || got.TrustedRootPath != endpoints.TrustedRootPath {
		t.Errorf("sigstore endpoints not carried to sign-blob: %+v", got)
	}

	if _, kmsErr := provenanceSignBlobInput(signProvenanceInput{output: "p.json", method: "kms", keyRef: "hashivault://k", endpoints: endpoints}, "b"); !errors.Is(kmsErr, errs.ErrUsage) {
		t.Fatalf("kms with --fulcio-url: err = %v, want ErrUsage (documented as forbidden for --method=kms)", kmsErr)
	}

	kms, err := provenanceSignBlobInput(signProvenanceInput{output: "p.json", method: "kms", keyRef: "hashivault://k", endpoints: signflags.Endpoints{TrustedRootPath: "trusted_root.json"}}, "b")
	if err != nil {
		t.Fatalf("kms with only --trusted-root: %v", err)
	}

	if kms.TrustedRootPath != "trusted_root.json" {
		t.Errorf("kms TrustedRootPath = %q, want the flag value", kms.TrustedRootPath)
	}
}

func TestProvenanceSignBlobInput_MapsSigstoreToKeylessAndKMSToKey(t *testing.T) {
	t.Parallel()

	t.Run("sigstore_is_keyless", func(t *testing.T) {
		t.Parallel()

		got, err := provenanceSignBlobInput(signProvenanceInput{output: "p.json", method: "sigstore", oidcIssuer: "https://issuer"}, "p.json.bundle")
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}

		if !got.Keyless || got.KeyRef != "" || got.OIDCIssuer != "https://issuer" || got.BundlePath != "p.json.bundle" {
			t.Errorf("sigstore mapping wrong: %+v", got)
		}
	})

	t.Run("kms_uses_key", func(t *testing.T) {
		t.Parallel()

		got, err := provenanceSignBlobInput(signProvenanceInput{output: "p.json", method: "kms", keyRef: "hashivault://k"}, "b")
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}

		if got.Keyless || got.KeyRef != "hashivault://k" {
			t.Errorf("kms mapping wrong: %+v", got)
		}
	})

	t.Run("bare_key_defaults_to_kms", func(t *testing.T) {
		t.Parallel()

		got, err := provenanceSignBlobInput(signProvenanceInput{output: "p.json", keyRef: "env://K"}, "b")
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}

		if got.KeyRef != "env://K" || got.Keyless {
			t.Errorf("bare-key mapping wrong: %+v", got)
		}
	})

	t.Run("kms_without_key_errors", func(t *testing.T) {
		t.Parallel()

		if _, err := provenanceSignBlobInput(signProvenanceInput{output: "p.json", method: "kms"}, "b"); !errors.Is(err, errs.ErrUsage) {
			t.Fatalf("err = %v, want ErrUsage", err)
		}
	})

	t.Run("gpg_rejected", func(t *testing.T) {
		t.Parallel()

		if _, err := provenanceSignBlobInput(signProvenanceInput{output: "p.json", method: "gpg"}, "b"); !errors.Is(err, errs.ErrUsage) {
			t.Fatalf("err = %v, want ErrUsage", err)
		}
	})
}

func TestParseProvenanceProfile_DefaultsToGenericAndRejectsUnknown(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "generic"} {
		got, err := parseProvenanceProfile(raw)
		if err != nil {
			t.Fatalf("parseProvenanceProfile(%q): %v", raw, err)
		}

		if got != apprelease.ProvenanceProfileGeneric {
			t.Errorf("parseProvenanceProfile(%q) = %q", raw, got)
		}
	}

	got, err := parseProvenanceProfile("forgejo-actions")
	if err != nil {
		t.Fatalf("forgejo-actions: %v", err)
	}

	if got != apprelease.ProvenanceProfileForgejoActions {
		t.Errorf("forgejo-actions = %q", got)
	}

	if _, err := parseProvenanceProfile("forgejo"); !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("unknown profile err = %v, want ErrUsage", err)
	}
}

func TestForgejoActionsBuilderID_BuildsTheWorkflowURIAtTheTag(t *testing.T) {
	t.Parallel()

	got := forgejoActionsBuilderID("https://codeberg.org/o/r/", "v1.2.3", "release.yml")

	want := "https://codeberg.org/o/r/.forgejo/workflows/release.yml@v1.2.3"
	if got != want {
		t.Fatalf("builder id = %q, want %q", got, want)
	}
}

func TestResolveStartedOn_PrefersExplicitOverTheCommitTimestamp(t *testing.T) {
	t.Run("explicit_rfc3339_wins", func(t *testing.T) {
		fake := &fakeCommitUnixTimer{epoch: "0"}

		got, err := resolveStartedOn(context.Background(), fake, "2026-06-01T00:00:00Z", "HEAD")
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}

		if got != "2026-06-01T00:00:00Z" {
			t.Fatalf("started-on = %q", got)
		}

		if fake.called {
			t.Fatal("commit timestamp should not be read when --started-on is explicit")
		}
	})

	t.Run("commit_ref_becomes_rfc3339", func(t *testing.T) {
		fake := &fakeCommitUnixTimer{epoch: "0"}

		got, err := resolveStartedOn(context.Background(), fake, "", "release-sha")
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}

		if got != "1970-01-01T00:00:00Z" {
			t.Fatalf("started-on = %q", got)
		}

		if fake.ref != "release-sha" {
			t.Fatalf("commit ref = %q", fake.ref)
		}
	})

	t.Run("commit_timestamp_must_be_unix_seconds", func(t *testing.T) {
		_, err := resolveStartedOn(context.Background(), &fakeCommitUnixTimer{epoch: "not-a-time"}, "", "HEAD")
		if !errors.Is(err, errs.ErrUsage) {
			t.Fatalf("err = %v, want ErrUsage", err)
		}
	})

	t.Run("source_date_epoch_fallback", func(t *testing.T) {
		t.Setenv("SOURCE_DATE_EPOCH", "0")

		got, err := resolveStartedOn(context.Background(), &fakeCommitUnixTimer{}, "", "")
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}

		if got != "1970-01-01T00:00:00Z" {
			t.Fatalf("started-on = %q", got)
		}
	})

	t.Run("missing_timestamp_errors", func(t *testing.T) {
		t.Setenv("SOURCE_DATE_EPOCH", "")

		_, err := resolveStartedOn(context.Background(), &fakeCommitUnixTimer{}, "", "")
		if !errors.Is(err, errs.ErrUsage) {
			t.Fatalf("err = %v, want ErrUsage", err)
		}
	})
}

type fakeCommitUnixTimer struct {
	epoch  string
	err    error
	ref    string
	called bool
}

func (f *fakeCommitUnixTimer) CommitUnixTime(_ context.Context, ref string) (string, error) {
	f.called = true
	f.ref = ref

	return f.epoch, f.err
}

// TestProvenanceCommand_ExposesExternalParametersFlag pins the generic
// externalParameters-extras flag on `release provenance`, the successor
// to the per-field lineage flags slated for the coordinated flip.
func TestProvenanceCommand_ExposesExternalParametersFlag(t *testing.T) {
	t.Parallel()

	for _, flag := range provenanceCmd().Flags {
		for _, name := range flag.Names() {
			if name == flagExternalParametersJSON {
				return
			}
		}
	}

	t.Fatalf("release provenance missing --%s", flagExternalParametersJSON)
}
