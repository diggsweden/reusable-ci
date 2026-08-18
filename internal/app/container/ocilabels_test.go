// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

type fakeLabelsRegistry struct {
	ref    string
	labels map[string]string
}

func (f *fakeLabelsRegistry) Labels(_ context.Context, ref string) (map[string]string, error) {
	f.ref = ref

	return f.labels, nil
}

func TestOCIReleaseLabelFlags_EmitsBuildahTokensAndDefaultsDocumentation(t *testing.T) {
	t.Parallel()

	got, err := appcontainer.OCIReleaseLabelFlags(appcontainer.OCIReleaseLabelsInput{
		OCILabels: domaincontainer.OCILabels{
			Title:       "nanolinter",
			Version:     "v0.7.9",
			Revision:    strings.Repeat("a", 40),
			RefName:     "v0.7.9-alpine",
			Description: "Small lint image",
			Licenses:    "EUPL-1.2",
			Vendor:      "Itiquette",
			Authors:     "The Itiquette Authors",
		},
		Created: "2026-06-30T12:34:56Z",
		Source:  "https://codeberg.org/Itiquette/nanolinter",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"--label", "org.opencontainers.image.title=nanolinter",
		"--label", "org.opencontainers.image.version=v0.7.9",
		"--label", "org.opencontainers.image.created=2026-06-30T12:34:56Z",
		"--label", "org.opencontainers.image.revision=" + strings.Repeat("a", 40),
		"--label", "org.opencontainers.image.ref.name=v0.7.9-alpine",
		"--label", "org.opencontainers.image.source=https://codeberg.org/Itiquette/nanolinter",
		"--label", "org.opencontainers.image.url=https://codeberg.org/Itiquette/nanolinter",
		"--label", "org.opencontainers.image.documentation=https://codeberg.org/Itiquette/nanolinter#readme",
		"--label", "org.opencontainers.image.description=Small lint image",
		"--label", "org.opencontainers.image.licenses=EUPL-1.2",
		"--label", "org.opencontainers.image.vendor=Itiquette",
		"--label", "org.opencontainers.image.authors=The Itiquette Authors",
	}

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("label flags = %#v, want %#v", got, want)
	}
}

func TestOCIReleaseLabelFlags_RejectsMissingRequiredLabel(t *testing.T) {
	t.Parallel()

	_, err := appcontainer.OCIReleaseLabelFlags(appcontainer.OCIReleaseLabelsInput{})
	if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "org.opencontainers.image.title") {
		t.Fatalf("err = %v, want missing title usage error", err)
	}
}

// TestOCIReleaseIdentityMatches_ToleratesCaseOnlyInTheSource pins how much
// drift this predicate forgives. The source is compared case-insensitively,
// because forge owner names drift in case; revision, version and ref.name are
// exact.
//
// The second half of that rule was never tested. Three cases covered a match,
// a wrong revision and a wrong repository path, so a predicate that folded
// case everywhere -- accepting an image whose version differs only in case --
// would have passed.
func TestOCIReleaseIdentityMatches_ToleratesCaseOnlyInTheSource(t *testing.T) {
	t.Parallel()

	commit := strings.Repeat("b", 40)
	labels := `{"org.opencontainers.image.revision":"` + commit +
		`","org.opencontainers.image.version":"v0.7.9","org.opencontainers.image.ref.name":"v0.7.9-alpine",` +
		`"org.opencontainers.image.source":"https://codeberg.org/Itiquette/nanolinter"}`

	expected := appcontainer.OCIReleaseIdentityInput{
		Revision: commit,
		Version:  "v0.7.9",
		RefName:  "v0.7.9-alpine",
		Source:   "https://codeberg.org/Itiquette/nanolinter",
	}

	// Each row changes one field of an otherwise matching identity.
	for name, testCase := range map[string]struct {
		mutate func(*appcontainer.OCIReleaseIdentityInput)
		want   bool
	}{
		"identical": {mutate: func(*appcontainer.OCIReleaseIdentityInput) {}, want: true},
		"source owner case drift": {mutate: func(in *appcontainer.OCIReleaseIdentityInput) {
			in.Source = "https://codeberg.org/itiquette/nanolinter"
		}, want: true},
		"source host case drift": {mutate: func(in *appcontainer.OCIReleaseIdentityInput) {
			in.Source = "https://CODEBERG.ORG/Itiquette/nanolinter"
		}, want: true},

		"different repository path": {mutate: func(in *appcontainer.OCIReleaseIdentityInput) { in.Source = "https://codeberg.org/other/nanolinter" }, want: false},
		"different revision":        {mutate: func(in *appcontainer.OCIReleaseIdentityInput) { in.Revision = strings.Repeat("c", 40) }, want: false},
		"different version":         {mutate: func(in *appcontainer.OCIReleaseIdentityInput) { in.Version = "v0.7.10" }, want: false},
		"different ref name":        {mutate: func(in *appcontainer.OCIReleaseIdentityInput) { in.RefName = "v0.7.9-debian" }, want: false},

		// The tolerance is the source's alone. A digest, a version or a tag
		// differing only in case is a different identity, not drift.
		"revision case drift": {mutate: func(in *appcontainer.OCIReleaseIdentityInput) { in.Revision = strings.ToUpper(commit) }, want: false},
		"version case drift":  {mutate: func(in *appcontainer.OCIReleaseIdentityInput) { in.Version = "V0.7.9" }, want: false},
		"ref name case drift": {mutate: func(in *appcontainer.OCIReleaseIdentityInput) { in.RefName = "V0.7.9-Alpine" }, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			in := expected
			testCase.mutate(&in)

			match, err := appcontainer.OCIReleaseIdentityMatches(labels, in)
			if err != nil {
				t.Fatal(err)
			}

			if match != testCase.want {
				t.Errorf("match = %v, want %v for %+v", match, testCase.want, in)
			}
		})
	}
}

func TestOCIReleaseIdentityMatches_RejectsMalformedLabelsJSON(t *testing.T) {
	t.Parallel()

	_, err := appcontainer.OCIReleaseIdentityMatches(`{`, appcontainer.OCIReleaseIdentityInput{})
	if !errors.Is(err, errs.ErrMalformedInput) {
		t.Fatalf("err = %v, want ErrMalformedInput", err)
	}
}

func TestOCIImageLabelsJSON_FetchesCompactLabelsObject(t *testing.T) {
	t.Parallel()

	registry := &fakeLabelsRegistry{labels: map[string]string{
		"org.opencontainers.image.revision": "abc123",
		"org.opencontainers.image.version":  "v1.2.3",
	}}

	got, err := appcontainer.OCIImageLabelsJSON(context.Background(), registry, "example.invalid/ns/app:v1.2.3")
	if err != nil {
		t.Fatal(err)
	}

	if got != `{"org.opencontainers.image.revision":"abc123","org.opencontainers.image.version":"v1.2.3"}` {
		t.Fatalf("labels JSON = %s", got)
	}

	if registry.ref != "example.invalid/ns/app:v1.2.3" {
		t.Fatalf("registry ref = %q", registry.ref)
	}
}
