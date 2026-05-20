// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"encoding/json"
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

	want := []string{"ghcr.io/o/r:v1.0.0", "ghcr.io/o/r:1.0"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}

	for i := range got {
		if got[i] != want[i] {
			t.Errorf("tags[%d] = %q want %q", i, got[i], want[i])
		}
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
	// Round-trip into a map for shape assertion.
	var got struct {
		Tags   []string          `json:"tags"`
		Labels map[string]string `json:"labels"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}

	if len(got.Tags) != 2 || got.Tags[0] != "img:v1.0.0" {
		t.Errorf("Tags = %v", got.Tags)
	}

	if got.Labels["org.opencontainers.image.licenses"] != "MIT" {
		t.Errorf("Labels = %v", got.Labels)
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
