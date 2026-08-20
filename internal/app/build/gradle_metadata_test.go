// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestGradleMetadata_EmitsVersion(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("gradle.properties", []byte("org.gradle.jvmargs=-Xmx1g\nversion=1.2.3\n"))

	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	if err := appbuild.GradleMetadata(context.Background(), sink, &out, output.Annotator{}, appbuild.GradleMetadataInput{Dir: fsys.Root}); err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("version"); got != "1.2.3" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("version = %q", got)
	}

	if !strings.Contains(out.String(), "Version: 1.2.3") {
		t.Errorf("out = %s", out.String())
	}
}

func TestGradleMetadata_WarnsWhenMissing(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("gradle.properties", []byte("org.gradle.jvmargs=-Xmx1g\n"))

	sink := fakeoutputsink.New(t)

	var stderr bytes.Buffer

	if err := appbuild.GradleMetadata(context.Background(), sink, &bytes.Buffer{}, output.NewAnnotator(&stderr, output.FormatGitHub), appbuild.GradleMetadataInput{Dir: fsys.Root}); err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("version"); got != "" {
		t.Errorf("version = %q", got)
	}

	if !strings.Contains(stderr.String(), "version not found") {
		t.Errorf("stderr = %s", stderr.String())
	}
}

// is-snapshot mirrors `build maven metadata` so the gradle and maven
// publish summaries stay symmetrical.
func TestGradleMetadata_EmitsIsSnapshot(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"1.2.3":          "false",
		"1.2.3-SNAPSHOT": "true",
	}
	for version, want := range cases {
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			fsys := testfs.NewReal(t)
			fsys.WriteFile("gradle.properties", []byte("version="+version+"\n"))

			sink := fakeoutputsink.New(t)

			if err := appbuild.GradleMetadata(context.Background(), sink, &bytes.Buffer{}, output.Annotator{}, appbuild.GradleMetadataInput{Dir: fsys.Root}); err != nil {
				t.Fatal(err)
			}

			if got := sink.Single("is-snapshot"); got != want {
				t.Errorf("is-snapshot for %q = %q, want %q", version, got, want)
			}
		})
	}
}

// A project computing its version in build.gradle.kts must not emit a
// half-populated summary: no version means no is-snapshot either.
func TestGradleMetadata_NoVersionEmitsNoIsSnapshot(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewReal(t)
	fsys.WriteFile("gradle.properties", []byte("org.gradle.jvmargs=-Xmx1g\n"))

	sink := fakeoutputsink.New(t)

	var stderr bytes.Buffer

	if err := appbuild.GradleMetadata(context.Background(), sink, &bytes.Buffer{}, output.NewAnnotator(&stderr, output.FormatGitHub), appbuild.GradleMetadataInput{Dir: fsys.Root}); err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("is-snapshot"); got != "" {
		t.Errorf("is-snapshot = %q, want unset", got)
	}
}
