// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

type fakeAttestor struct {
	got          cosign.AttestImageInput
	predicateRaw string // contents of the predicate file at call time
	called       bool
}

func (f *fakeAttestor) AttestImage(_ context.Context, in cosign.AttestImageInput, _ io.Writer) error {
	f.called = true
	f.got = in

	if b, err := os.ReadFile(in.PredicatePath); err == nil {
		f.predicateRaw = string(b)
	}

	return nil
}

func TestAttestImage_SBOMWithExplicitPredicate(t *testing.T) {
	t.Parallel()

	sbom := writeTemp(t, `{"bomFormat":"CycloneDX"}`)
	att := &fakeAttestor{}

	err := appcontainer.AttestImage(context.Background(), att, &bytes.Buffer{}, appcontainer.AttestImageInput{
		Image: "ghcr.io/o/r@sha256:abc", Method: domainrelease.SignMethodSigstore,
		PredicateType: "cyclonedx", PredicatePath: sbom, Recursive: true,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}

	if att.got.PredicateType != "cyclonedx" || att.got.PredicatePath != sbom || !att.got.Keyless || !att.got.Recursive {
		t.Errorf("attestor input wrong: %+v", att.got)
	}
}

func TestAttestImage_GeneratesSLSAProvenance(t *testing.T) {
	t.Parallel()

	att := &fakeAttestor{}

	err := appcontainer.AttestImage(context.Background(), att, &bytes.Buffer{}, appcontainer.AttestImageInput{
		Image: "ghcr.io/o/r@sha256:abc", Method: domainrelease.SignMethodSigstore,
		PredicateType: "slsaprovenance1",
		Provenance: provenance.Input{
			BuildType: provenance.ContainerBuildType,
			BuilderID: "https://github.com/o/r/.github/workflows/x.yml@refs/tags/v1",
			SourceURI: "git+https://github.com/o/r",
			ResolvedDeps: []provenance.Dependency{
				provenance.SourceDependency("https://github.com/o/r", "refs/tags/v1", "deadbeef"),
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}

	if !strings.Contains(att.predicateRaw, "buildDefinition") || !strings.Contains(att.predicateRaw, "deadbeef") {
		t.Errorf("generated predicate missing SLSA fields:\n%s", att.predicateRaw)
	}

	// The generated predicate is SLSA v1.0, so cosign must be told
	// "slsaprovenance1" — bare "slsaprovenance" is cosign's v0.2 alias and
	// would mislabel the statement and break v1.0 verifiers.
	if att.got.PredicateType != "slsaprovenance1" {
		t.Errorf("cosign PredicateType = %q, want slsaprovenance1 (v1.0)", att.got.PredicateType)
	}
}

// TestAttestImage_RejectsSLSAv02 proves only SLSA v1.0 is supported: cosign's
// obsolete bare "slsaprovenance" (v0.2) alias is refused with a usage error.
func TestAttestImage_RejectsSLSAv02(t *testing.T) {
	t.Parallel()

	att := &fakeAttestor{}

	err := appcontainer.AttestImage(context.Background(), att, &bytes.Buffer{}, appcontainer.AttestImageInput{
		Image: "ghcr.io/o/r@sha256:abc", Method: domainrelease.SignMethodSigstore, PredicateType: "slsaprovenance",
	})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage (v0.2 unsupported)", err)
	}

	// fakeAttestor has always recorded this and nothing ever read it: a
	// refusal that still signed would satisfy every error check above.
	if att.called {
		t.Error("attested the image despite refusing the request")
	}
}

func TestAttestImage_NonSLSATypeRequiresPredicate(t *testing.T) {
	t.Parallel()

	att := &fakeAttestor{}

	err := appcontainer.AttestImage(context.Background(), att, &bytes.Buffer{}, appcontainer.AttestImageInput{
		Image: "ghcr.io/o/r@sha256:abc", Method: domainrelease.SignMethodSigstore, PredicateType: "cyclonedx",
	})
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("err = %v, want ErrMissingInput", err)
	}

	// fakeAttestor has always recorded this and nothing ever read it: a
	// refusal that still signed would satisfy every error check above.
	if att.called {
		t.Error("attested the image despite refusing the request")
	}
}

func TestAttestImage_RejectsGPG(t *testing.T) {
	t.Parallel()

	att := &fakeAttestor{}

	err := appcontainer.AttestImage(context.Background(), att, &bytes.Buffer{}, appcontainer.AttestImageInput{
		Image: "ghcr.io/o/r@sha256:abc", Method: domainrelease.SignMethodGPG,
		PredicateType: "cyclonedx", PredicatePath: writeTemp(t, "{}"),
	})
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}

	// fakeAttestor has always recorded this and nothing ever read it: a
	// refusal that still signed would satisfy every error check above.
	if att.called {
		t.Error("attested the image despite refusing the request")
	}
}

func writeTemp(t *testing.T, body string) string {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "pred-*.json")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.WriteString(body); err != nil {
		t.Fatal(err)
	}

	_ = f.Close()

	return f.Name()
}

// TestAttestImage_ForwardsTheWholeRequestForEachMethod compares the request
// cosign receives, whole, against one built independently here. Sigstore
// carries every endpoint and the trust root; KMS carries the key. A field
// given for the other method is forwarded too, so the request's own
// validation refuses it rather than the use case dropping it silently: a
// Rekor URL passed with kms used to vanish, sending the entry to cosign's
// default log instead of the one named.
func TestAttestImage_ForwardsTheWholeRequestForEachMethod(t *testing.T) {
	t.Parallel()

	image := "ghcr.io/o/r@sha256:" + strings.Repeat("a", 64)
	sbom := writeTemp(t, `{"bomFormat":"CycloneDX"}`)

	for name, tc := range map[string]struct {
		in      appcontainer.AttestImageInput
		want    cosign.AttestImageInput
		refused bool
	}{
		"sigstore with every endpoint": {
			in: appcontainer.AttestImageInput{
				Image: image, Method: domainrelease.SignMethodSigstore, PredicateType: "cyclonedx", PredicatePath: sbom, Recursive: true,
				OIDCIssuer: "https://issuer.example.internal", FulcioURL: "https://fulcio.example.internal",
				RekorURL: "https://rekor.example.internal", TrustedRootPath: "trusted-root.json",
			},
			want: cosign.AttestImageInput{
				ImageRef: image, PredicateType: "cyclonedx", PredicatePath: sbom, Recursive: true, Keyless: true,
				OIDCIssuer: "https://issuer.example.internal", FulcioURL: "https://fulcio.example.internal",
				RekorURL: "https://rekor.example.internal", TrustedRootPath: "trusted-root.json",
			},
		},
		"kms": {
			in:   appcontainer.AttestImageInput{Image: image, Method: domainrelease.SignMethodKMS, PredicateType: "cyclonedx", PredicatePath: sbom, KeyRef: "awskms:///alias/release"},
			want: cosign.AttestImageInput{ImageRef: image, PredicateType: "cyclonedx", PredicatePath: sbom, KeyRef: "awskms:///alias/release"},
		},
		"kms with a rekor url": {
			in:      appcontainer.AttestImageInput{Image: image, Method: domainrelease.SignMethodKMS, PredicateType: "cyclonedx", PredicatePath: sbom, KeyRef: "awskms:///alias/release", RekorURL: "https://rekor.example.internal"},
			want:    cosign.AttestImageInput{ImageRef: image, PredicateType: "cyclonedx", PredicatePath: sbom, KeyRef: "awskms:///alias/release", RekorURL: "https://rekor.example.internal"},
			refused: true,
		},
		"sigstore with a key": {
			in:      appcontainer.AttestImageInput{Image: image, Method: domainrelease.SignMethodSigstore, PredicateType: "cyclonedx", PredicatePath: sbom, KeyRef: "awskms:///alias/release"},
			want:    cosign.AttestImageInput{ImageRef: image, PredicateType: "cyclonedx", PredicatePath: sbom, Keyless: true, KeyRef: "awskms:///alias/release"},
			refused: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			att := &fakeAttestor{}

			if err := appcontainer.AttestImage(context.Background(), att, io.Discard, tc.in); err != nil {
				t.Fatal(err)
			}

			if att.got != tc.want {
				t.Errorf("request =\n%+v\nwant\n%+v", att.got, tc.want)
			}

			if err := att.got.Validate(); tc.refused != errors.Is(err, errs.ErrUsage) || (!tc.refused && err != nil) {
				t.Errorf("request validation = %v, want refused %v", err, tc.refused)
			}
		})
	}
}

// TestAttestImage_GeneratedPredicateIsExactAndRemoved decodes the SLSA
// predicate cosign is handed and compares it whole, then checks the temporary
// file is gone once attestation returns. The generation test above only looks
// for two substrings, which a predicate missing its builder or run details
// would still contain.
func TestAttestImage_GeneratedPredicateIsExactAndRemoved(t *testing.T) {
	t.Parallel()

	att := &fakeAttestor{}

	err := appcontainer.AttestImage(context.Background(), att, io.Discard, appcontainer.AttestImageInput{
		Image: "ghcr.io/o/r@sha256:" + strings.Repeat("a", 64), Method: domainrelease.SignMethodKMS,
		KeyRef: "awskms:///alias/release", PredicateType: "slsaprovenance1",
		Provenance: provenance.Input{
			BuildType:    provenance.ContainerBuildType,
			BuilderID:    "https://github.com/o/r/.github/workflows/x.yml@refs/tags/v1",
			SourceURI:    "git+https://github.com/o/r",
			Ref:          "refs/tags/v1",
			ImageName:    "ghcr.io/o/r",
			InvocationID: "https://github.com/o/r/actions/runs/7",
			ResolvedDeps: []provenance.Dependency{provenance.SourceDependency("https://github.com/o/r", "refs/tags/v1", "deadbeef")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	var got any
	if err := json.Unmarshal([]byte(att.predicateRaw), &got); err != nil {
		t.Fatalf("predicate is not JSON: %v\n%s", err, att.predicateRaw)
	}

	want := map[string]any{
		"buildDefinition": map[string]any{
			"buildType":          "https://diggsweden.github.io/reusable-ci/container-build/v1",
			"externalParameters": map[string]any{"image": "ghcr.io/o/r", "ref": "refs/tags/v1", "source": "git+https://github.com/o/r"},
			"internalParameters": map[string]any{},
			"resolvedDependencies": []any{
				map[string]any{"uri": "git+https://github.com/o/r@refs/tags/v1", "digest": map[string]any{"gitCommit": "deadbeef"}},
			},
		},
		"runDetails": map[string]any{
			"builder":  map[string]any{"id": "https://github.com/o/r/.github/workflows/x.yml@refs/tags/v1"},
			"metadata": map[string]any{"invocationId": "https://github.com/o/r/actions/runs/7"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("predicate =\n%s\nwant\n%#v", att.predicateRaw, want)
	}

	if _, statErr := os.Stat(att.got.PredicatePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("generated predicate %s survived the attestation (stat err %v)", att.got.PredicatePath, statErr)
	}
}

// TestAttestImage_RefusesTheMethodBeforeGeneratingAPredicate covers the order
// of the checks. The method used to be checked after the predicate was
// generated, so gpg or a missing method with an incomplete provenance input
// reported the provenance problem and wrote a temporary file first.
func TestAttestImage_RefusesTheMethodBeforeGeneratingAPredicate(t *testing.T) {
	t.Parallel()

	for method, want := range map[domainrelease.SignMethod]error{
		domainrelease.SignMethodGPG: errs.ErrInvalidConfig,
		"":                          errs.ErrMissingInput,
	} {
		att := &fakeAttestor{}

		err := appcontainer.AttestImage(context.Background(), att, io.Discard, appcontainer.AttestImageInput{
			Image: "ghcr.io/o/r@sha256:" + strings.Repeat("a", 64), Method: method, PredicateType: "slsaprovenance1",
		})
		if !errors.Is(err, want) || att.called {
			t.Errorf("method %q: err = %v, attested = %v; want %v and no attestation", method, err, att.called, want)
		}
	}
}
