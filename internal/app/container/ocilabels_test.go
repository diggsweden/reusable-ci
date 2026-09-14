// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

type fakeLabelsRegistry struct {
	ref    string
	labels map[string]string
	err    error
	calls  int
}

func (f *fakeLabelsRegistry) Labels(_ context.Context, ref string) (map[string]string, error) {
	f.ref = ref
	f.calls++

	return f.labels, f.err
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

	if !slices.Equal(got, want) {
		t.Fatalf("label flags = %#v, want %#v", got, want)
	}
}

// TestOCIReleaseLabelFlags_OmitsOptionalLabelsWithNoValue covers the other half
// of the builder's rule. The eight required labels are refused when empty; the
// four optional ones are left out instead, so an image never carries a label
// claiming its description or licence is the empty string.
//
// Only the refusing half had a test.
func TestOCIReleaseLabelFlags_OmitsOptionalLabelsWithNoValue(t *testing.T) {
	t.Parallel()

	got, err := appcontainer.OCIReleaseLabelFlags(appcontainer.OCIReleaseLabelsInput{
		OCILabels: domaincontainer.OCILabels{
			Title:    "nanolinter",
			Version:  "v0.7.9",
			Revision: strings.Repeat("a", 40),
			RefName:  "v0.7.9-alpine",
			// Description, Licenses, Vendor and Authors deliberately unset.
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
	}
	if !slices.Equal(got, want) {
		t.Errorf("label flags = %#v\nwant %#v", got, want)
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

func TestOCIReleaseIdentityMatches_RejectsIncompleteExpectedIdentity(t *testing.T) {
	t.Parallel()

	complete := appcontainer.OCIReleaseIdentityInput{
		Revision: "abc123",
		Version:  "v1.2.3",
		RefName:  "v1.2.3",
		Source:   "https://example.org/org/app",
	}

	for name, mutate := range map[string]func(*appcontainer.OCIReleaseIdentityInput){
		"missing revision": func(in *appcontainer.OCIReleaseIdentityInput) { in.Revision = "" },
		"missing version":  func(in *appcontainer.OCIReleaseIdentityInput) { in.Version = "" },
		"missing ref name": func(in *appcontainer.OCIReleaseIdentityInput) { in.RefName = "" },
		"missing source":   func(in *appcontainer.OCIReleaseIdentityInput) { in.Source = " " },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			in := complete
			mutate(&in)

			matched, err := appcontainer.OCIReleaseIdentityMatches(`{}`, in)

			if matched || !errors.Is(err, errs.ErrUsage) {
				t.Fatalf("matched, err = %v, %v; want false, ErrUsage", matched, err)
			}
		})
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

// TestOCIImageLabelsJSON_UnlabelledImageIsAnEmptyObject covers an image that
// carries no labels. The registry adapter returns a nil map and the JSON must
// still be an object: a consumer decoding `null` into a map gets nil and reads
// every label as absent-and-empty, which is indistinguishable from an image
// whose labels were all blank. The code converts explicitly, and nothing
// checked it.
func TestOCIImageLabelsJSON_UnlabelledImageIsAnEmptyObject(t *testing.T) {
	t.Parallel()

	got, err := appcontainer.OCIImageLabelsJSON(context.Background(), &fakeLabelsRegistry{}, "example.invalid/ns/app:v1.2.3")
	if err != nil {
		t.Fatal(err)
	}

	if got != `{}` {
		t.Errorf("labels JSON = %s, want an empty object", got)
	}
}

// validOCIReleaseLabelsInput is a complete input, so each case below can remove
// exactly one field.
func validOCIReleaseLabelsInput() appcontainer.OCIReleaseLabelsInput {
	return appcontainer.OCIReleaseLabelsInput{
		OCILabels: domaincontainer.OCILabels{
			Title:    "Demo",
			Version:  "v1.2.3",
			Revision: strings.Repeat("a", 40),
			RefName:  "v1.2.3",
		},
		Created: "2026-01-01T00:00:00Z",
		Source:  "https://codeberg.org/example/demo",
	}
}

// TestOCIReleaseLabels_NamesEveryMissingRequiredLabel checks each required
// label individually.
//
// The existing refusal test passes an entirely empty input, which fails on the
// first entry in the list — the title — and says nothing about the other seven.
// Removing any of them from the required set leaves it passing, and the ones
// that matter most are the provenance fields: an image published without
// revision, source or ref.name is one whose origin cannot be established from
// the image itself, which is the reason these labels are mandatory here.
func TestOCIReleaseLabels_NamesEveryMissingRequiredLabel(t *testing.T) {
	t.Parallel()

	for label, blank := range map[string]func(in *appcontainer.OCIReleaseLabelsInput){
		"org.opencontainers.image.title":    func(in *appcontainer.OCIReleaseLabelsInput) { in.Title = "" },
		"org.opencontainers.image.version":  func(in *appcontainer.OCIReleaseLabelsInput) { in.Version = "" },
		"org.opencontainers.image.created":  func(in *appcontainer.OCIReleaseLabelsInput) { in.Created = "" },
		"org.opencontainers.image.revision": func(in *appcontainer.OCIReleaseLabelsInput) { in.Revision = "" },
		"org.opencontainers.image.ref.name": func(in *appcontainer.OCIReleaseLabelsInput) { in.RefName = "" },
		"org.opencontainers.image.source":   func(in *appcontainer.OCIReleaseLabelsInput) { in.Source = "" },
	} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()

			in := validOCIReleaseLabelsInput()
			blank(&in)

			_, err := appcontainer.OCIReleaseLabels(in)
			if !errors.Is(err, errs.ErrUsage) {
				t.Fatalf("err = %v, want ErrUsage when %s is empty", err, label)
			}

			if !strings.Contains(err.Error(), label) {
				t.Errorf("err = %v, want it to name %s", err, label)
			}
		})
	}

	// The complete input must pass, or every case above is satisfied by a
	// function that refuses unconditionally.
	if _, err := appcontainer.OCIReleaseLabels(validOCIReleaseLabelsInput()); err != nil {
		t.Fatalf("a complete input was refused: %v", err)
	}
}

// TestOCIReleaseLabels_RefusesMultilineValues covers the other guard in the
// same loop, which no case reaches.
//
// Labels are rendered as `--label key=value` arguments, so a value carrying a
// newline is an extra argument the operator did not write. Source is the one an
// adopter supplies most freely, and every required label goes through the same
// check.
func TestOCIReleaseLabels_RefusesMultilineValues(t *testing.T) {
	t.Parallel()

	for _, injected := range []string{
		"https://codeberg.org/example/demo\n--label=injected=1",
		"https://codeberg.org/example/demo\r--label=injected=1",
	} {
		in := validOCIReleaseLabelsInput()
		in.Source = injected

		_, err := appcontainer.OCIReleaseLabels(in)
		if !errors.Is(err, errs.ErrUsage) {
			t.Fatalf("err = %v, want ErrUsage for a multi-line label value", err)
		}

		if !strings.Contains(err.Error(), "single line") {
			t.Errorf("err = %v, want it to name the single-line rule", err)
		}
	}
}

// TestOCIImageLabelsJSON_RefusesBadInputWithoutTouchingTheRegistry is the same
// rule on the label reader, whose refusals had no coverage at all.
func TestOCIImageLabelsJSON_RefusesBadInputWithoutTouchingTheRegistry(t *testing.T) {
	t.Parallel()

	t.Run("a blank ref", func(t *testing.T) {
		t.Parallel()

		registry := &fakeLabelsRegistry{}

		got, err := appcontainer.OCIImageLabelsJSON(context.Background(), registry, "")
		if !errors.Is(err, errs.ErrUsage) {
			t.Fatalf("err = %v, want ErrUsage", err)
		}

		if got != "" {
			t.Errorf("a refused call returned %q", got)
		}

		if registry.calls != 0 {
			t.Errorf("the registry was queried %d time(s) for a blank ref", registry.calls)
		}
	})

	t.Run("a nil registry", func(t *testing.T) {
		t.Parallel()

		if _, err := appcontainer.OCIImageLabelsJSON(context.Background(), nil, "ghcr.io/org/app:v1"); !errors.Is(err, errs.ErrUsage) {
			t.Errorf("err = %v, want ErrUsage", err)
		}
	})

	t.Run("the registry itself fails", func(t *testing.T) {
		t.Parallel()

		registry := &fakeLabelsRegistry{err: errRegistryUnreachable}

		got, err := appcontainer.OCIImageLabelsJSON(context.Background(), registry, "ghcr.io/org/app:v1")
		if !errors.Is(err, errRegistryUnreachable) {
			t.Fatalf("err = %v, want the registry's own cause", err)
		}

		if got != "" {
			t.Errorf("a failed read returned %q", got)
		}
	})
}
