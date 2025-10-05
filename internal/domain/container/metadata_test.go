// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
)

func TestLabel_String(t *testing.T) {
	t.Parallel()

	label := container.Label{Key: "org.opencontainers.image.title", Value: "app"} //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	if got := label.String(); got != "org.opencontainers.image.title=app" {
		t.Errorf("String() = %q", got)
	}
}

func TestPrimaryVersion_HighestPriorityWins(t *testing.T) {
	t.Parallel()
	// sha (100) declared before semver (900) — semver still wins.
	applied := []container.AppliedTag{
		{Tag: "sha-abcdef0", Priority: 100},
		{Tag: "1.0.0", Priority: 900}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}
	if got := container.PrimaryVersion(applied); got != "1.0.0" {
		t.Errorf("PrimaryVersion = %q, want 1.0.0", got)
	}
}

func TestPrimaryVersion_TieGoesToFirstDeclared(t *testing.T) {
	t.Parallel()

	applied := []container.AppliedTag{
		{Tag: "1.0.0", Priority: 900},
		{Tag: "1.0", Priority: 900},
		{Tag: "1", Priority: 900},
	}
	if got := container.PrimaryVersion(applied); got != "1.0.0" {
		t.Errorf("PrimaryVersion = %q, want 1.0.0 (first declared)", got)
	}
}

func TestPrimaryVersion_Empty(t *testing.T) {
	t.Parallel()

	if got := container.PrimaryVersion(nil); got != "" {
		t.Errorf("PrimaryVersion(nil) = %q", got)
	}
}

func TestFormatTags_PrefixesImage(t *testing.T) {
	t.Parallel()

	got := container.FormatTags("ghcr.io/o/r", []container.AppliedTag{
		{Tag: "v1.0.0"}, {Tag: "1.0"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})

	if want := []string{"ghcr.io/o/r:v1.0.0", "ghcr.io/o/r:1.0"}; !slices.Equal(got, want) {
		t.Errorf("FormatTags = %v, want %v", got, want)
	}
}

func TestBuildLabels_TitleIsLastPathSegment(t *testing.T) {
	t.Parallel()

	labels := container.BuildLabels(container.LabelInputs{
		ImageName: "ghcr.io/example/app",
		RepoURL:   "https://github.com/example/app", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		SHA:       "abcdef0123",
		Primary:   "main", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	want := map[string]string{
		"org.opencontainers.image.title":    "app",
		"org.opencontainers.image.url":      "https://github.com/example/app",
		"org.opencontainers.image.source":   "https://github.com/example/app",
		"org.opencontainers.image.version":  "main",
		"org.opencontainers.image.revision": "abcdef0123",
		"org.opencontainers.image.created":  "2026-01-01T00:00:00Z",
	}

	got := map[string]string{}
	for _, l := range labels {
		got[l.Key] = l.Value
	}

	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestBuildLabels_DescriptionAndLicensePassthrough(t *testing.T) {
	t.Parallel()

	labels := container.BuildLabels(container.LabelInputs{
		ImageName:   "ghcr.io/example/app",
		Description: "A test image",
		License:     "Apache-2.0",
		CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})

	got := map[string]string{}
	for _, l := range labels {
		got[l.Key] = l.Value
	}

	if got["org.opencontainers.image.description"] != "A test image" {
		t.Errorf("description = %q", got["org.opencontainers.image.description"])
	}

	if got["org.opencontainers.image.licenses"] != "Apache-2.0" {
		t.Errorf("licenses = %q", got["org.opencontainers.image.licenses"])
	}
}

func TestBuildJSONOutput_Shape(t *testing.T) {
	t.Parallel()

	jo := container.BuildJSONOutput(
		[]string{"img:v1.0.0", "img:1"},
		[]container.Label{
			{Key: "org.opencontainers.image.licenses", Value: "MIT"},
			{Key: "org.opencontainers.image.title", Value: "app"},
		},
	)

	b, err := json.Marshal(jo) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatal(err)
	}
	// Pin every public key and value, including tag order and the complete labels.
	want := `{"tags":["img:v1.0.0","img:1"],"labels":{"org.opencontainers.image.licenses":"MIT","org.opencontainers.image.title":"app"}}`
	if string(b) != want {
		t.Errorf("JSON = %s, want %s", b, want)
	}
}

func TestBuildJSONOutput_EmptyTagsRendersArrayNotNull(t *testing.T) {
	t.Parallel()

	jo := container.BuildJSONOutput(nil, nil)

	b, err := json.Marshal(jo) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Tags   []string          `json:"tags"`
		Labels map[string]string `json:"labels"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}

	if got.Tags == nil || len(got.Tags) != 0 {
		t.Errorf("tags = %#v, want empty non-nil array", got.Tags)
	}

	if got.Labels == nil || len(got.Labels) != 0 {
		t.Errorf("labels = %#v, want empty object", got.Labels)
	}
}

// TestBuildLabels_ExactOrderedLabels pins the whole slice, in order.
//
// The tests above convert the result to a map and check that the keys they
// expect are present. A map cannot see what that hides: an extra label nobody
// asked for, a duplicate key (the map keeps one), or a change in order. Order
// matters because these labels are rendered into build arguments in sequence,
// and cardinality matters because a duplicated key is a Containerfile that
// declares the same label twice with different values.
func TestBuildLabels_ExactOrderedLabels(t *testing.T) {
	t.Parallel()

	got := container.BuildLabels(container.LabelInputs{
		ImageName:   "ghcr.io/example/app",
		Description: "an example",
		RepoURL:     "https://github.com/example/app",
		Primary:     "v1.2.3",
		SHA:         "abcdef0123",
		License:     "EUPL-1.2",
		CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})

	want := []container.Label{
		{Key: "org.opencontainers.image.title", Value: "app"},
		{Key: "org.opencontainers.image.description", Value: "an example"},
		{Key: "org.opencontainers.image.url", Value: "https://github.com/example/app"},
		{Key: "org.opencontainers.image.source", Value: "https://github.com/example/app"},
		{Key: "org.opencontainers.image.version", Value: "v1.2.3"},
		{Key: "org.opencontainers.image.created", Value: "2026-01-01T00:00:00Z"},
		{Key: "org.opencontainers.image.revision", Value: "abcdef0123"},
		{Key: "org.opencontainers.image.licenses", Value: "EUPL-1.2"},
	}

	if !slices.Equal(got, want) {
		t.Errorf("labels =\n%v\nwant\n%v", got, want)
	}
}

// TestBuildLabels_OmitsUnknownValuesEntirely covers the omission policy at
// exact cardinality.
//
// An absent label says nothing; an empty one claims the value IS the empty
// string, which is what published images carried on every build whose event
// context had no commit. Counting the result is what distinguishes "omitted"
// from "present and empty" — a map lookup returns "" for both.
func TestBuildLabels_OmitsUnknownValuesEntirely(t *testing.T) {
	t.Parallel()

	got := container.BuildLabels(container.LabelInputs{
		ImageName: "ghcr.io/example/app",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})

	want := []container.Label{
		{Key: "org.opencontainers.image.title", Value: "app"},
		{Key: "org.opencontainers.image.created", Value: "2026-01-01T00:00:00Z"},
	}

	if !slices.Equal(got, want) {
		t.Errorf("labels =\n%v\nwant only title and created\n%v", got, want)
	}
}

// TestBuildLabels_CreatedIsRenderedInUTC crosses a date boundary, which is the
// only way to see the conversion happen.
//
// Every other case passes a time already in UTC, so dropping the .UTC() call
// changes nothing they can observe. A build running at 01:30 in Stockholm on
// the 2nd is 23:30 UTC on the 1st, and a label claiming the wrong DAY is the
// kind of provenance error nobody notices until it is used as evidence.
func TestBuildLabels_CreatedIsRenderedInUTC(t *testing.T) {
	t.Parallel()

	// UTC+2, so 01:30 local on the 2nd is 23:30 UTC on the 1st.
	zone := time.FixedZone("CEST", 2*60*60)

	got := container.BuildLabels(container.LabelInputs{
		ImageName: "app",
		CreatedAt: time.Date(2026, 6, 2, 1, 30, 0, 0, zone),
	})

	const want = "2026-06-01T23:30:00Z"

	for _, label := range got {
		if label.Key != "org.opencontainers.image.created" {
			continue
		}

		if label.Value != want {
			t.Errorf("created = %q, want %q (the local time was 2026-06-02T01:30 at UTC+2)", label.Value, want)
		}

		return
	}

	t.Fatal("no created label was emitted")
}
