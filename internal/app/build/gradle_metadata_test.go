// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestGradleMetadata_EmitsVersion(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		props string
		want  string
	}{
		{
			name:  "plain assignment",
			props: "org.gradle.jvmargs=-Xmx1g\nversion=1.2.3\n",
			want:  "1.2.3",
		},
		{
			name:  "value is trimmed",
			props: "version=  1.2.3  \n",
			want:  "1.2.3",
		},
		{
			name:  "indented line still counts",
			props: "  version=1.2.3\n",
			want:  "1.2.3",
		},
		{
			// A longer key must not be mistaken for this one: Android
			// projects carry versionCode and versionName alongside version.
			name:  "a longer key is not this key",
			props: "versionCode=42\nversionName=nine\nversion=1.2.3\n",
			want:  "1.2.3",
		},
		{
			name:  "a commented version is not read",
			props: "#version=9.9.9\nversion=1.2.3\n",
			want:  "1.2.3",
		},
		{
			name:  "first assignment wins",
			props: "version=1.2.3\nversion=4.5.6\n",
			want:  "1.2.3",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fsys := testfs.NewReal(t)
			fsys.WriteFile("gradle.properties", []byte(tc.props))

			sink := fakeoutputsink.New(t)

			var out bytes.Buffer

			if err := appbuild.GradleMetadata(context.Background(), sink, &out, output.Annotator{}, appbuild.GradleMetadataInput{Dir: fsys.Root}); err != nil {
				t.Fatal(err)
			}

			if got := sink.Single("version"); got != tc.want {
				t.Errorf("version = %q, want %q", got, tc.want)
			}

			if got, want := out.String(), "Version: "+tc.want+"\n"; got != want {
				t.Errorf("out = %q, want %q", got, want)
			}
		})
	}
}

func TestGradleMetadata_WarnsWhenMissing(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		props string // "" writes no gradle.properties at all
	}{
		{name: "no version key", props: "org.gradle.jvmargs=-Xmx1g\n"},
		{name: "no gradle.properties", props: ""},
		{name: "version key with no value", props: "version=\n"},
		{
			// java.util.Properties accepts "version = 1.2.3", and this
			// parser does not -- such a project is treated as versionless
			// rather than refused. Recorded in docs/open-questions.md.
			name:  "spaced assignment is not recognised",
			props: "version = 1.2.3\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fsys := testfs.NewReal(t)
			if tc.props != "" {
				fsys.WriteFile("gradle.properties", []byte(tc.props))
			}

			sink := fakeoutputsink.New(t)

			var stderr, out bytes.Buffer

			if err := appbuild.GradleMetadata(context.Background(), sink, &out, output.NewAnnotator(&stderr, output.FormatGitHub), appbuild.GradleMetadataInput{Dir: fsys.Root}); err != nil {
				t.Fatal(err)
			}

			// Keys, not Single: Single returns "" for an absent key and for
			// a key set to "", so it cannot tell "emitted nothing" from
			// "emitted an empty version" -- and a consumer reading an empty
			// version output behaves differently from one reading none.
			if got := sink.Keys(); len(got) != 0 {
				t.Errorf("emitted %q, want nothing", got)
			}

			if out.Len() != 0 {
				t.Errorf("wrote %q, want nothing", out.String())
			}

			if !strings.Contains(stderr.String(), "version not found") {
				t.Errorf("stderr = %s", stderr.String())
			}
		})
	}
}
