// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
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

	err := appcontainer.AttestImage(context.Background(), &fakeAttestor{}, &bytes.Buffer{}, appcontainer.AttestImageInput{
		Image: "ghcr.io/o/r@sha256:abc", Method: domainrelease.SignMethodSigstore, PredicateType: "slsaprovenance",
	})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage (v0.2 unsupported)", err)
	}
}

func TestAttestImage_NonSLSATypeRequiresPredicate(t *testing.T) {
	t.Parallel()

	err := appcontainer.AttestImage(context.Background(), &fakeAttestor{}, &bytes.Buffer{}, appcontainer.AttestImageInput{
		Image: "ghcr.io/o/r@sha256:abc", Method: domainrelease.SignMethodSigstore, PredicateType: "cyclonedx",
	})
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("err = %v, want ErrMissingInput", err)
	}
}

func TestAttestImage_RejectsGPG(t *testing.T) {
	t.Parallel()

	err := appcontainer.AttestImage(context.Background(), &fakeAttestor{}, &bytes.Buffer{}, appcontainer.AttestImageInput{
		Image: "ghcr.io/o/r@sha256:abc", Method: domainrelease.SignMethodGPG,
		PredicateType: "cyclonedx", PredicatePath: writeTemp(t, "{}"),
	})
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
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
