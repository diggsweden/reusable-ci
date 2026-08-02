// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"context"
	"encoding/json"
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

	tags := sink.Multiline("tags")

	want := []string{testImage + ":v1.0.0", testImage + ":1.0.0", testImage + ":1"}
	if len(tags) != len(want) {
		t.Fatalf("tags = %v", tags)
	}

	for i := range want {
		if tags[i] != want[i] {
			t.Errorf("tags[%d] = %q, want %q", i, tags[i], want[i])
		}
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
}

func TestComputeMetadata_LabelsWithOverrides_SkipFetch(t *testing.T) {
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

	labels := sink.Multiline("labels")
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
	got := map[string]string{}

	for _, line := range labels {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}

	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	// Both overrides set → no FetchRepoMetadata call.
	if c := prov.Calls().FetchRepoMetadata; c != 0 {
		t.Errorf("FetchRepoMetadata calls = %d, want 0 when both overrides set", c)
	}
}

func TestComputeMetadata_LabelsFetchOnMissingFields(t *testing.T) {
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

	labels := sink.Multiline("labels")
	want := map[string]string{
		"org.opencontainers.image.description": "fetched description",
		"org.opencontainers.image.licenses":    "MIT",
	}
	got := map[string]string{}

	for _, line := range labels {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}

	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
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
	// description / license should be empty in the labels.
	labels := sink.Multiline("labels")
	got := map[string]string{}

	for _, line := range labels {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}

	if got["org.opencontainers.image.description"] != "" {
		t.Errorf("description should be empty after fetch failure, got %q",
			got["org.opencontainers.image.description"])
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

	if len(doc.Tags) != 2 || doc.Tags[0] != testImage+":1.0.0" {
		t.Errorf("Tags = %v", doc.Tags)
	}

	if doc.Labels["org.opencontainers.image.licenses"] != "MIT" {
		t.Errorf("Labels = %v", doc.Labels)
	}
}

type fakeError string

func (e fakeError) Error() string { return string(e) }
