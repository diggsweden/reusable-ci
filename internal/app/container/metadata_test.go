// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakemanifestsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
)

const testImage = "ghcr.io/example/app"

func fixedTime() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}

func newFake(t *testing.T, evt provider.EventContext) *fakeprovider.Fake {
	t.Helper()

	return fakeprovider.New(t).WithEventContext(evt)
}

func TestComputeMetadata_RequiresImageName(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t)
	sink := fakeoutputsink.New(t)

	_, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, nil, appcontainer.ComputeMetadataInput{})
	if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "image name is required") {
		t.Errorf("err = %v, want ErrUsage naming the missing image", err)
	}

	if got := sink.Keys(); len(got) != 0 {
		t.Errorf("emitted %q with no image name", got)
	}
}

func TestComputeMetadata_RawTag(t *testing.T) {
	t.Parallel()
	prov := newFake(t, provider.EventContext{})
	sink := fakeoutputsink.New(t)

	_, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, nil, appcontainer.ComputeMetadataInput{
		ImageName: testImage,
		TagRules:  "type=raw,value=main,enable=true",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := sink.Multiline("tags"); len(got) != 1 || got[0] != testImage+":main" {
		t.Errorf("tags = %v", got)
	}
}

func TestComputeMetadata_BranchTag(t *testing.T) {
	t.Parallel()
	prov := newFake(t, provider.EventContext{
		RefName: "develop",
		RefType: provider.RefTypeBranch,
	})
	sink := fakeoutputsink.New(t)

	_, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, nil, appcontainer.ComputeMetadataInput{
		ImageName: testImage,
		TagRules:  "type=ref,event=branch",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := sink.Multiline("tags"); len(got) != 1 || got[0] != testImage+":develop" {
		t.Errorf("tags = %v", got)
	}
}

func TestComputeMetadata_NoTagsEmitsEmptyScalar(t *testing.T) {
	t.Parallel()
	prov := newFake(t, provider.EventContext{
		RefName: "v1.0.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		RefType: provider.RefTypeTag,
	})
	sink := fakeoutputsink.New(t)
	// branch rule + tag ref → silent skip → no tags.
	_, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, nil, appcontainer.ComputeMetadataInput{
		ImageName: testImage,
		TagRules:  "type=ref,event=branch",
	})
	if err != nil {
		t.Fatal(err)
	}
	// No multiline tags entry, scalar tags should be present and empty.
	if got := sink.Multiline("tags"); len(got) != 0 {
		t.Errorf("expected empty multiline tags, got %v", got)
	}

	if got, ok := sink.AllScalar()["tags"]; !ok || got != "" {
		t.Errorf("scalar tags = %q ok=%v", got, ok)
	}
}

func TestComputeMetadata_MultipleRulesInDeclarationOrder(t *testing.T) {
	t.Parallel()
	prov := newFake(t, provider.EventContext{
		RefName: "v1.0.0",
		RefType: provider.RefTypeTag,
	})
	sink := fakeoutputsink.New(t)
	rules := strings.Join([]string{
		"type=ref,event=tag,enable=true",
		"type=semver,pattern={{version}},enable=true",
		"type=semver,pattern={{major}},enable=true",
	}, "\n")

	_, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, nil, appcontainer.ComputeMetadataInput{
		ImageName: testImage,
		TagRules:  rules,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Declared ref, then semver version, then semver major. Sorting these
	// would give 1, 1.0.0, v1.0.0 -- a different order, so the fixture can
	// tell the rule it tests from the obvious alternative.
	want := []string{testImage + ":v1.0.0", testImage + ":1.0.0", testImage + ":1"}
	if got := sink.Multiline("tags"); !slices.Equal(got, want) {
		t.Errorf("tags = %v, want %v", got, want)
	}
}

func TestComputeMetadata_ExplicitReleaseIdentityOverridesRequestEvent(t *testing.T) {
	t.Parallel()

	prov := newFake(t, provider.EventContext{
		RefName:  "release-request/v1.2.3",
		RefType:  provider.RefTypeTag,
		SHA:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ShortSHA: "aaaaaaa",
	})
	sink := fakeoutputsink.New(t)

	_, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, nil, appcontainer.ComputeMetadataInput{
		ImageName:      testImage,
		TagRules:       "type=ref,event=tag\ntype=semver,pattern={{version}}",
		SourceRefName:  "v1.2.3",
		SourceRefType:  provider.RefTypeTag,
		SourceRevision: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{testImage + ":v1.2.3", testImage + ":1.2.3"}
	if got := sink.Multiline("tags"); !slices.Equal(got, want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}

	if got := sink.Single("source-ref-name"); got != "v1.2.3" {
		t.Fatalf("source-ref-name = %q, want normalized release identity", got)
	}
}

// TestComputeMetadata_PromotionOutputsFollowTheRefKind pins the three outputs
// the publish workflow gates promotion on. Only a tag whose name is already a
// valid OCI tag is promotable: a branch or pull request with a clean name is
// not, and a tag the sanitizer would rewrite is not either, because the
// staging tag and ledger are built from the raw name. staging-tag is written
// empty rather than left out, so the workflow can read it unconditionally.
func TestComputeMetadata_PromotionOutputsFollowTheRefKind(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		evt  provider.EventContext
		in   appcontainer.ComputeMetadataInput
		want map[string]string
	}{
		"clean tag": {
			evt:  provider.EventContext{RefName: "v1.2.3", RefType: provider.RefTypeTag},
			want: map[string]string{"source-ref-name": "v1.2.3", "ref-clean": "true", "staging-tag": testImage + ":staging-v1.2.3"},
		},
		"tag the sanitizer would rewrite": {
			evt:  provider.EventContext{RefName: "release/v1.2.3", RefType: provider.RefTypeTag},
			want: map[string]string{"source-ref-name": "release/v1.2.3", "ref-clean": "false", "staging-tag": ""},
		},
		"branch with a clean name": {
			evt:  provider.EventContext{RefName: "main", RefType: provider.RefTypeBranch},
			want: map[string]string{"source-ref-name": "main", "ref-clean": "false", "staging-tag": ""},
		},
		"pull request with a clean name": {
			evt:  provider.EventContext{RefName: "v1.2.3", RefType: provider.RefTypePR, PRNumber: "42"},
			want: map[string]string{"source-ref-name": "v1.2.3", "ref-clean": "false", "staging-tag": ""},
		},
		"release identity overrides a branch event": {
			evt:  provider.EventContext{RefName: "release-request/v2.0.0", RefType: provider.RefTypeBranch},
			in:   appcontainer.ComputeMetadataInput{SourceRefName: "v2.0.0", SourceRefType: provider.RefTypeTag},
			want: map[string]string{"source-ref-name": "v2.0.0", "ref-clean": "true", "staging-tag": testImage + ":staging-v2.0.0"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prov := newFake(t, tc.evt)
			sink := fakeoutputsink.New(t)

			in := tc.in
			in.ImageName = testImage
			in.TagRules = "type=raw,value=main"

			if _, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, nil, in); err != nil {
				t.Fatal(err)
			}

			got := sink.AllScalar()
			for key, want := range tc.want {
				if value, present := got[key]; !present || value != want {
					t.Errorf("%s = %q (present %v), want %q", key, value, present, want)
				}
			}
		})
	}
}

func TestComputeMetadata_PrimaryByPriority(t *testing.T) {
	t.Parallel()
	prov := newFake(t, provider.EventContext{
		RefName:  "v1.0.0",
		RefType:  provider.RefTypeTag,
		ShortSHA: "abcdef0",
	})
	sink := fakeoutputsink.New(t)
	// sha (100) declared first; semver (900) wins primary.
	rules := "type=sha,prefix=sha-,enable=true\ntype=semver,pattern={{version}},enable=true"

	_, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, nil, appcontainer.ComputeMetadataInput{
		ImageName: testImage,
		TagRules:  rules,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("version"); got != "1.0.0" {
		t.Errorf("version = %q, want 1.0.0", got)
	}

	// Priority decides which tag is primary, not which rules apply. Dropping
	// the lower-priority rule would leave the version right and the release
	// short a tag, which asserting the version alone could not see.
	want := []string{testImage + ":sha-abcdef0", testImage + ":1.0.0"}
	if got := sink.Multiline("tags"); !slices.Equal(got, want) {
		t.Errorf("tags = %v, want both rules applied in declaration order: %v", got, want)
	}
}

// labelsFromSink parses the multi-line labels output into key/value pairs.
// Three tests were each doing this inline.
func labelsFromSink(t *testing.T, sink *fakeoutputsink.Sink) map[string]string {
	t.Helper()

	got := map[string]string{}

	for _, line := range sink.Multiline("labels") {
		key, value, _ := strings.Cut(line, "=")
		got[key] = value
	}

	return got
}

// TestComputeMetadata_OverriddenFieldsAreNotFetched covers the supplied side of
// the rule: a description and licence given on the input are used as-is and the
// forge is not asked for them.
//
// The labels are compared as a whole set. They ship on the published image, so
// one appearing that nobody asked for is worth seeing, and checking only the
// expected keys cannot.
func TestComputeMetadata_OverriddenFieldsAreNotFetched(t *testing.T) {
	t.Parallel()
	prov := newFake(t, provider.EventContext{
		Repo:    "example/app",                    //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		RepoURL: "https://github.com/example/app", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		SHA:     "abcdef0123",
	})
	sink := fakeoutputsink.New(t)

	_, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, nil, appcontainer.ComputeMetadataInput{
		ImageName:   testImage,
		TagRules:    "type=raw,value=main", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		EmitLabels:  true,
		Description: "A test image",
		License:     "Apache-2.0",
		Now:         fixedTime(),
	})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"org.opencontainers.image.title":       "app",
		"org.opencontainers.image.description": "A test image",
		"org.opencontainers.image.licenses":    "Apache-2.0",
		"org.opencontainers.image.url":         "https://github.com/example/app",
		"org.opencontainers.image.source":      "https://github.com/example/app",
		"org.opencontainers.image.revision":    "abcdef0123",
		"org.opencontainers.image.version":     "main",
		"org.opencontainers.image.created":     "2026-01-01T00:00:00Z",
	}
	if got := labelsFromSink(t, sink); !maps.Equal(got, want) {
		t.Errorf("labels = %v\nwant %v", got, want)
	}

	// Both fields were supplied, so there is nothing to look up. The fetch is
	// a network call against the forge; skipping it is the point of the
	// overrides, not an incidental saving.
	if c := prov.Calls().FetchRepoMetadata; c != 0 {
		t.Errorf("FetchRepoMetadata calls = %d, want 0 when both overrides are set", c)
	}
}

// TestComputeMetadata_MissingFieldsComeFromTheForge is the other side: with
// neither field supplied, both are fetched once from the repository named in
// the event context.
func TestComputeMetadata_MissingFieldsComeFromTheForge(t *testing.T) {
	t.Parallel()
	prov := newFake(t, provider.EventContext{
		Repo:    "example/app",
		RepoURL: "https://github.com/example/app",
	}).WithRepoMetadata(provider.RepoMetadata{
		Description: "fetched description",
		LicenseSPDX: "MIT", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	sink := fakeoutputsink.New(t)

	_, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, nil, appcontainer.ComputeMetadataInput{
		ImageName:  testImage,
		TagRules:   "type=raw,value=main",
		EmitLabels: true,
		Now:        fixedTime(),
	})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"org.opencontainers.image.title":       "app",
		"org.opencontainers.image.description": "fetched description",
		"org.opencontainers.image.licenses":    "MIT",
		"org.opencontainers.image.url":         "https://github.com/example/app",
		"org.opencontainers.image.source":      "https://github.com/example/app",
		"org.opencontainers.image.version":     "main",
		"org.opencontainers.image.created":     "2026-01-01T00:00:00Z",
		// No revision: this fixture has no SHA, and a label whose value is
		// unknown is left out rather than published empty.
	}
	if got := labelsFromSink(t, sink); !maps.Equal(got, want) {
		t.Errorf("labels = %v\nwant %v", got, want)
	}

	if c := prov.Calls().FetchRepoMetadata; c != 1 {
		t.Errorf("FetchRepoMetadata calls = %d, want 1", c)
	}

	if args := prov.FetchRepoArgs(); len(args) != 1 || args[0] != "example/app" {
		t.Errorf("FetchRepo args = %v", args)
	}
}

// TestComputeMetadata_OneSidedOverrideKeepsTheOverrideAndFetchesTheRest
// covers the two mixed cases between the pair above, which had only the
// all-or-nothing ends.
//
// Description and license are independent overrides resolved by one helper
// against one fetch, and the fetch happens whenever EITHER is missing. That
// makes the mixed case the one where the two can be confused: the supplied
// value has to survive a lookup that also returns a value for the same field,
// and the fetch still has to happen for the other. Both ends are satisfied by
// a helper that simply prefers whatever the forge said.
func TestComputeMetadata_OneSidedOverrideKeepsTheOverrideAndFetchesTheRest(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		description string
		license     string
		wantDesc    string
		wantLicense string
	}{
		"description supplied, license fetched": {
			description: "operator description",
			wantDesc:    "operator description",
			wantLicense: "MIT",
		},
		"license supplied, description fetched": {
			license:     "EUPL-1.2",
			wantDesc:    "fetched description",
			wantLicense: "EUPL-1.2",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// The forge answers with a value for BOTH fields, so a helper that
			// let the fetch win would produce a different label rather than an
			// empty one.
			prov := newFake(t, provider.EventContext{
				Repo:    "example/app",
				RepoURL: "https://github.com/example/app",
				SHA:     "abcdef0123",
			}).WithRepoMetadata(provider.RepoMetadata{
				Description: "fetched description",
				LicenseSPDX: "MIT",
			})
			sink := fakeoutputsink.New(t)

			if _, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, nil, appcontainer.ComputeMetadataInput{
				ImageName:   testImage,
				TagRules:    "type=raw,value=main",
				EmitLabels:  true,
				Description: tc.description,
				License:     tc.license,
				Now:         fixedTime(),
			}); err != nil {
				t.Fatal(err)
			}

			want := map[string]string{
				"org.opencontainers.image.title":       "app",
				"org.opencontainers.image.description": tc.wantDesc,
				"org.opencontainers.image.licenses":    tc.wantLicense,
				"org.opencontainers.image.url":         "https://github.com/example/app",
				"org.opencontainers.image.source":      "https://github.com/example/app",
				"org.opencontainers.image.revision":    "abcdef0123",
				"org.opencontainers.image.version":     "main",
				"org.opencontainers.image.created":     "2026-01-01T00:00:00Z",
			}
			if got := labelsFromSink(t, sink); !maps.Equal(got, want) {
				t.Errorf("labels = %v\nwant %v", got, want)
			}

			// Once, not twice: the helper resolves both fields from a single
			// lookup, and one network call per missing field is the shape this
			// would silently degrade into.
			if c := prov.Calls().FetchRepoMetadata; c != 1 {
				t.Errorf("FetchRepoMetadata calls = %d, want exactly 1", c)
			}
		})
	}
}

// TestComputeMetadata_LabelsFetchErrorIsNonFatal requires a failed lookup to
// change nothing but the two fields it would have filled. The same build runs
// against a forge that answers with no description or licence, and every
// output of the two runs must match: tags, version, the promotion outputs,
// the JSON document and the remaining labels.
//
// Absence alone was checked before, and reading a missing key returns "", so
// a fetch error that also dropped the tags or the created label passed.
func TestComputeMetadata_LabelsFetchErrorIsNonFatal(t *testing.T) {
	t.Parallel()

	evt := provider.EventContext{
		Repo:    "example/app",
		RepoURL: "https://github.com/example/app",
		RefName: "v1.2.3",
		RefType: provider.RefTypeTag,
		SHA:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	in := appcontainer.ComputeMetadataInput{
		ImageName:  testImage,
		TagRules:   "type=raw,value=main",
		EmitLabels: true,
		Now:        fixedTime(),
	}

	run := func(prov *fakeprovider.Fake) *fakeoutputsink.Sink {
		t.Helper()

		sink := fakeoutputsink.New(t)
		if _, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, nil, in); err != nil {
			t.Fatalf("API error should be non-fatal, got: %v", err)
		}

		if c := prov.Calls().FetchRepoMetadata; c != 1 {
			t.Errorf("FetchRepoMetadata calls = %d, want 1", c)
		}

		return sink
	}

	failed := run(newFake(t, evt).WithFetchRepoMetadataError(fakeError("boom")))
	empty := run(newFake(t, evt).WithRepoMetadata(provider.RepoMetadata{}))

	wantKeys := []string{"json", "labels", "ref-clean", "source-ref-name", "staging-tag", "tags", "version"}
	if got := failed.Keys(); !slices.Equal(got, wantKeys) {
		t.Errorf("outputs = %v, want %v", got, wantKeys)
	}

	if got, want := failed.AllScalar(), empty.AllScalar(); !maps.Equal(got, want) {
		t.Errorf("scalar outputs after a failed fetch =\n%v\nwant\n%v", got, want)
	}

	for _, key := range []string{"tags", "labels"} {
		if got, want := failed.Multiline(key), empty.Multiline(key); !slices.Equal(got, want) {
			t.Errorf("%s after a failed fetch = %v, want %v", key, got, want)
		}
	}

	got := labelsFromSink(t, failed)
	for _, key := range []string{"org.opencontainers.image.description", "org.opencontainers.image.licenses"} {
		if value, present := got[key]; present {
			t.Errorf("%s = %q, want the label omitted after a failed fetch", key, value)
		}
	}

	if got["org.opencontainers.image.created"] != "2026-01-01T00:00:00Z" || failed.Single("staging-tag") != testImage+":staging-v1.2.3" {
		t.Errorf("labels = %v, staging-tag = %q; want the unaffected outputs kept", got, failed.Single("staging-tag"))
	}
}

// TestComputeMetadata_LabelsSkippedWhenEmitLabelsFalse runs with a repository
// in the event context, so the forge lookup is reachable and its absence is
// the gate at work rather than a missing repository.
func TestComputeMetadata_LabelsSkippedWhenEmitLabelsFalse(t *testing.T) {
	t.Parallel()

	prov := newFake(t, provider.EventContext{
		Repo:    "example/app",
		RepoURL: "https://github.com/example/app",
		SHA:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}).WithRepoMetadata(provider.RepoMetadata{Description: "fetched description", LicenseSPDX: "MIT"})
	sink := fakeoutputsink.New(t)

	got, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, nil, appcontainer.ComputeMetadataInput{
		ImageName: testImage,
		TagRules:  "type=raw,value=main",
		Now:       fixedTime(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if c := prov.Calls().FetchRepoMetadata; c != 0 {
		t.Errorf("FetchRepoMetadata calls = %d, want 0 when labels are not emitted", c)
	}

	if slices.Contains(sink.Keys(), "labels") || len(got.Labels) != 0 || len(got.JSON.Labels) != 0 {
		t.Errorf("labels output = %v, result labels = %v, JSON labels = %v; want none", sink.Multiline("labels"), got.Labels, got.JSON.Labels)
	}

	if doc := sink.Single("json"); doc != `{"tags":["`+testImage+`:main"],"labels":{}}` {
		t.Errorf("json = %s, want no labels", doc)
	}
}

func TestComputeMetadata_FlavorLatestTrueRefused(t *testing.T) {
	t.Parallel()
	prov := newFake(t, provider.EventContext{})
	sink := fakeoutputsink.New(t)

	_, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, nil, appcontainer.ComputeMetadataInput{
		ImageName: testImage,
		TagRules:  "type=raw,value=main",
		Flavor:    "latest=true",
	})

	// ErrUnsupported, not a usage error: the flavour is well-formed and
	// this tool simply has no auto-:latest behaviour to give it, which the
	// exit ladder maps to a configuration code rather than a bad argument.
	if !errors.Is(err, errs.ErrUnsupported) || !strings.Contains(err.Error(), "latest=auto/true") {
		t.Errorf("err = %v, want ErrUnsupported naming the flavour", err)
	}
}

func TestComputeMetadata_FlavorLatestFalseAccepted(t *testing.T) {
	t.Parallel()
	prov := newFake(t, provider.EventContext{})
	sink := fakeoutputsink.New(t)

	_, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, nil, appcontainer.ComputeMetadataInput{
		ImageName: testImage,
		TagRules:  "type=raw,value=main",
		Flavor:    "latest=false",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	// Accepted *and* inert: latest=false is the supported spelling because
	// it asks for what this tool already does. A run that accepted it and
	// then added a :latest tag anyway would satisfy "no error" too.
	if want := []string{testImage + ":main"}; !slices.Equal(sink.Multiline("tags"), want) {
		t.Errorf("tags = %v, want %v with no :latest added", sink.Multiline("tags"), want)
	}
}

func TestComputeMetadata_JSONShape(t *testing.T) {
	t.Parallel()
	prov := newFake(t, provider.EventContext{
		RefName: "v1.0.0",
		RefType: provider.RefTypeTag,
		Repo:    "example/app",
	})
	sink := fakeoutputsink.New(t)
	rules := "type=semver,pattern={{version}}\ntype=semver,pattern={{major}}"

	_, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, nil, appcontainer.ComputeMetadataInput{
		ImageName:   testImage,
		TagRules:    rules,
		EmitLabels:  true,
		Description: "hi",
		License:     "MIT",
		Now:         fixedTime(),
	})
	if err != nil {
		t.Fatal(err)
	}

	jsonStr := sink.Single("json")

	var doc struct {
		Tags   []string          `json:"tags"`
		Labels map[string]string `json:"labels"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &doc); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, jsonStr)
	}

	// The JSON output is the same metadata as the individual outputs, for
	// consumers that want one document. Comparing it against them says that,
	// and covers the second tag and the other seven labels -- only Tags[0]
	// and one label were looked at.
	if want := sink.Multiline("tags"); !slices.Equal(doc.Tags, want) {
		t.Errorf("json tags = %v, want the tags output %v", doc.Tags, want)
	}

	if want := labelsFromSink(t, sink); !maps.Equal(doc.Labels, want) {
		t.Errorf("json labels = %v, want the labels output %v", doc.Labels, want)
	}

	if doc.Labels["org.opencontainers.image.licenses"] != "MIT" {
		t.Errorf("licence label = %q, want the override MIT", doc.Labels["org.opencontainers.image.licenses"])
	}
}

type fakeError string

func (e fakeError) Error() string { return string(e) }

// noMultilineSink is a recording sink that, like GitLab's dotenv sink,
// cannot hold a multi-line value and says so before writing anything.
type noMultilineSink struct{ *fakeoutputsink.Sink }

func (noMultilineSink) SetMultiline(context.Context, string, []string) error {
	return fmt.Errorf("dotenv holds no newlines: %w", errs.ErrUnsupported)
}

// countingManifest records every manifest write, which the fake manifest
// sink cannot show: it keeps only the latest body per stage.
type countingManifest struct {
	*fakemanifestsink.Sink

	writes int
}

func (c *countingManifest) Write(ctx context.Context, stage string, result map[string]any) error {
	c.writes++

	return c.Sink.Write(ctx, stage, result)
}

// TestComputeMetadata_EveryChannelCarriesOneResult runs one full metadata
// build through both kinds of output sink and compares every channel with
// the returned result: the scalar outputs and the order they are written in,
// the tag and label lists, the JSON document and the promotion outputs.
// Where the sink has no multi-line encoding, the tags and labels travel in
// exactly one stage manifest write holding both lists, and never partly in
// the sink.
func TestComputeMetadata_EveryChannelCarriesOneResult(t *testing.T) {
	t.Parallel()

	evt := provider.EventContext{
		RefName: "v1.2.3", RefType: provider.RefTypeTag, Repo: "example/app",
		RepoURL: "https://github.com/example/app", SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	in := appcontainer.ComputeMetadataInput{
		ImageName:   testImage,
		TagRules:    "type=ref,event=tag\ntype=semver,pattern={{version}}\ntype=raw,value=stable",
		EmitLabels:  true,
		Description: "an app",
		License:     "MIT",
		Now:         fixedTime(),
	}

	wantTags := []string{testImage + ":v1.2.3", testImage + ":1.2.3", testImage + ":stable"}
	wantLabels := []string{
		"org.opencontainers.image.title=app",
		"org.opencontainers.image.description=an app",
		"org.opencontainers.image.url=https://github.com/example/app",
		"org.opencontainers.image.source=https://github.com/example/app",
		"org.opencontainers.image.version=1.2.3",
		"org.opencontainers.image.created=2026-01-01T00:00:00Z",
		"org.opencontainers.image.revision=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"org.opencontainers.image.licenses=MIT",
	}
	wantJSON := `{"tags":["` + strings.Join(wantTags, `","`) + `"],"labels":{` +
		`"org.opencontainers.image.created":"2026-01-01T00:00:00Z","org.opencontainers.image.description":"an app",` +
		`"org.opencontainers.image.licenses":"MIT","org.opencontainers.image.revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",` +
		`"org.opencontainers.image.source":"https://github.com/example/app","org.opencontainers.image.title":"app",` +
		`"org.opencontainers.image.url":"https://github.com/example/app","org.opencontainers.image.version":"1.2.3"}}`
	wantScalars := map[string]string{
		"version": "1.2.3", "json": wantJSON,
		"source-ref-name": "v1.2.3", "ref-clean": "true", "staging-tag": testImage + ":staging-v1.2.3",
	}

	t.Run("native multi-line sink", func(t *testing.T) {
		t.Parallel()

		prov := newFake(t, evt)
		sink := fakeoutputsink.New(t)

		got, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, nil, in)
		if err != nil {
			t.Fatal(err)
		}

		assertMetadataResult(t, got, wantTags, wantLabels, wantJSON)

		if scalars := sink.AllScalar(); !maps.Equal(scalars, wantScalars) {
			t.Errorf("scalars =\n%v\nwant\n%v", scalars, wantScalars)
		}

		if !slices.Equal(sink.Multiline("tags"), wantTags) || !slices.Equal(sink.Multiline("labels"), wantLabels) {
			t.Errorf("tags = %q, labels = %q", sink.Multiline("tags"), sink.Multiline("labels"))
		}

		wantOrder := []string{"version", "tags", "labels", "json", "source-ref-name", "ref-clean", "staging-tag"}
		if order := sink.Order(); !slices.Equal(order, wantOrder) {
			t.Errorf("order = %q, want %q", order, wantOrder)
		}
	})

	t.Run("sink without multi-line", func(t *testing.T) {
		t.Parallel()

		prov := newFake(t, evt)
		sink := noMultilineSink{fakeoutputsink.New(t)}
		manifest := &countingManifest{Sink: fakemanifestsink.New(t)}

		got, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, manifest, in)
		if err != nil {
			t.Fatal(err)
		}

		assertMetadataResult(t, got, wantTags, wantLabels, wantJSON)

		if scalars := sink.AllScalar(); !maps.Equal(scalars, wantScalars) {
			t.Errorf("scalars =\n%v\nwant\n%v", scalars, wantScalars)
		}

		wantOrder := []string{"version", "json", "source-ref-name", "ref-clean", "staging-tag"}
		if order := sink.Order(); !slices.Equal(order, wantOrder) {
			t.Errorf("order = %q, want %q (no multi-line key may reach the sink)", order, wantOrder)
		}

		var body map[string][]string
		if err := json.Unmarshal([]byte(manifest.Body("container-metadata")), &body); err != nil {
			t.Fatalf("manifest body: %v", err)
		}

		if manifest.writes != 1 || !slices.Equal(manifest.Stages(), []string{"container-metadata"}) ||
			len(body) != 2 || !slices.Equal(body["tags"], wantTags) || !slices.Equal(body["labels"], wantLabels) {
			t.Errorf("manifest writes = %d, stages = %q, body = %v; want one container-metadata document with both lists", manifest.writes, manifest.Stages(), body)
		}
	})
}

func assertMetadataResult(t *testing.T, got *appcontainer.ComputeMetadataOutput, wantTags, wantLabels []string, wantJSON string) {
	t.Helper()

	labels := make([]string, 0, len(got.Labels))
	for _, label := range got.Labels {
		labels = append(labels, label.String())
	}

	doc, err := got.JSON.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(got.Tags, wantTags) || !slices.Equal(labels, wantLabels) || got.Primary != "1.2.3" || string(doc) != wantJSON {
		t.Errorf("result tags = %q, labels = %q, primary = %q, json = %s", got.Tags, labels, got.Primary, doc)
	}
}
