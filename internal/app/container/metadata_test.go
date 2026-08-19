// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
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
	if err == nil || !strings.Contains(err.Error(), "image name is required") {
		t.Errorf("err = %v", err)
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
	if got := sink.Multiline("tags"); !reflect.DeepEqual(got, want) {
		t.Errorf("tags = %v, want %v", got, want)
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
	if got := sink.Multiline("tags"); !reflect.DeepEqual(got, want) {
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
	if got := labelsFromSink(t, sink); !reflect.DeepEqual(got, want) {
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
	if got := labelsFromSink(t, sink); !reflect.DeepEqual(got, want) {
		t.Errorf("labels = %v\nwant %v", got, want)
	}

	if c := prov.Calls().FetchRepoMetadata; c != 1 {
		t.Errorf("FetchRepoMetadata calls = %d, want 1", c)
	}

	if args := prov.FetchRepoArgs(); len(args) != 1 || args[0] != "example/app" {
		t.Errorf("FetchRepo args = %v", args)
	}
}

func TestComputeMetadata_LabelsFetchErrorIsNonFatal(t *testing.T) {
	t.Parallel()
	prov := newFake(t, provider.EventContext{Repo: "example/app"})
	prov.WithFetchRepoMetadataError(fakeError("boom"))

	sink := fakeoutputsink.New(t)

	_, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, nil, appcontainer.ComputeMetadataInput{
		ImageName:  testImage,
		TagRules:   "type=raw,value=main",
		EmitLabels: true,
		Now:        fixedTime(),
	})
	if err != nil {
		t.Fatalf("API error should be non-fatal, got: %v", err)
	}
	// A failed lookup leaves out the fields it would have filled, rather than
	// aborting the whole metadata step. Absence is asserted directly: reading
	// a missing key returns "", so checking for an empty value would pass
	// whether the label was omitted or published blank.
	got := labelsFromSink(t, sink)
	for _, key := range []string{"org.opencontainers.image.description", "org.opencontainers.image.licenses"} {
		if value, present := got[key]; present {
			t.Errorf("%s = %q, want the label omitted after a failed fetch", key, value)
		}
	}
}

func TestComputeMetadata_LabelsSkippedWhenEmitLabelsFalse(t *testing.T) {
	t.Parallel()
	prov := newFake(t, provider.EventContext{})
	sink := fakeoutputsink.New(t)

	_, err := appcontainer.ComputeMetadata(context.Background(), prov, prov, sink, nil, appcontainer.ComputeMetadataInput{
		ImageName: testImage,
		TagRules:  "type=raw,value=main",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := sink.Multiline("labels"); len(got) != 0 {
		t.Errorf("labels should be empty when EmitLabels=false, got %v", got)
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
	if err == nil || !strings.Contains(err.Error(), "latest=auto/true") {
		t.Errorf("err = %v", err)
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
		t.Errorf("err = %v", err)
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
	if want := sink.Multiline("tags"); !reflect.DeepEqual(doc.Tags, want) {
		t.Errorf("json tags = %v, want the tags output %v", doc.Tags, want)
	}

	if want := labelsFromSink(t, sink); !reflect.DeepEqual(doc.Labels, want) {
		t.Errorf("json labels = %v, want the labels output %v", doc.Labels, want)
	}

	if doc.Labels["org.opencontainers.image.licenses"] != "MIT" {
		t.Errorf("licence label = %q, want the override MIT", doc.Labels["org.opencontainers.image.licenses"])
	}
}

type fakeError string

func (e fakeError) Error() string { return string(e) }
