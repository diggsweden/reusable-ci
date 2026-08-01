// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	apppublish "github.com/diggsweden/reusable-ci/v3/internal/app/publish"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestFindArtifact_EmitsFirstSortedMatch(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("artifacts/nested/z.ipa", []byte("second"))
	first := fsys.WriteFile("artifacts/a.ipa", []byte("first"))
	sink := fakeoutputsink.New(t)

	var out, stderr bytes.Buffer

	got, err := apppublish.FindArtifact(context.Background(), sink, &out, output.NewAnnotator(&stderr, output.FormatGitHub), apppublish.FindArtifactInput{
		Dir:       fsys.Path("artifacts"),
		Ext:       ".ipa",
		OutputKey: "ipa-file",
		Label:     "IPA",
		Recursive: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got != first {
		t.Errorf("selected = %q, want %q", got, first)
	}

	if sink.Single("ipa-file") != first {
		t.Errorf("ipa-file output = %q", sink.Single("ipa-file"))
	}

	if !strings.Contains(out.String(), "Found IPA") {
		t.Errorf("out = %s", out.String())
	}

	if !strings.Contains(stderr.String(), "Multiple IPA files") {
		t.Errorf("stderr = %s", stderr.String())
	}
}

func TestFindArtifact_NonRecursiveIgnoresNestedMatches(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("artifacts/nested/app.tgz", []byte("tarball"))

	sink := fakeoutputsink.New(t)

	_, err := apppublish.FindArtifact(context.Background(), sink, &bytes.Buffer{}, output.Annotator{}, apppublish.FindArtifactInput{
		Dir:       fsys.Path("artifacts"),
		Ext:       ".tgz",
		OutputKey: "tarball", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Label:     "tarball",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFindArtifact_MatchesCompoundExtension(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	archive := fsys.WriteFile("artifacts/app.tar.gz", []byte("tarball"))
	sink := fakeoutputsink.New(t)

	got, err := apppublish.FindArtifact(context.Background(), sink, &bytes.Buffer{}, output.Annotator{}, apppublish.FindArtifactInput{
		Dir:       fsys.Path("artifacts"),
		Ext:       ".tar.gz",
		OutputKey: "tarball",
		Label:     "tarball",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got != archive || sink.Single("tarball") != archive {
		t.Fatalf("selected=%q output=%q want %q", got, sink.Single("tarball"), archive)
	}
}

func TestFindArtifact_MatchesAnyExtension(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	archive := fsys.WriteFile("artifacts/app.tar.gz", []byte("tarball"))
	sink := fakeoutputsink.New(t)

	got, err := apppublish.FindArtifact(context.Background(), sink, &bytes.Buffer{}, output.Annotator{}, apppublish.FindArtifactInput{
		Dir:       fsys.Path("artifacts"),
		Exts:      []string{".tgz", ".tar.gz"},
		OutputKey: "tarball",
		Label:     "tarball",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got != archive {
		t.Fatalf("selected=%q want %q", got, archive)
	}
}

func TestFindArtifact_MissingEmitsAnnotation(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.MkdirAll("artifacts")

	sink := fakeoutputsink.New(t)

	var stderr bytes.Buffer

	err := func() error {
		_, err := apppublish.FindArtifact(context.Background(), sink, &bytes.Buffer{}, output.NewAnnotator(&stderr, output.FormatGitHub), apppublish.FindArtifactInput{
			Dir:       fsys.Path("artifacts"),
			Ext:       "aab",
			OutputKey: "aab-file",
			Label:     "AAB",
		})

		return err
	}()
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(stderr.String(), "No AAB file found") {
		t.Errorf("stderr = %s", stderr.String())
	}
}

func TestFindArtifact_RequiresOutputKey(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("artifacts/app.aab", []byte("aab"))

	sink := fakeoutputsink.New(t)

	_, err := apppublish.FindArtifact(context.Background(), sink, &bytes.Buffer{}, output.Annotator{}, apppublish.FindArtifactInput{
		Dir: fsys.Path("artifacts"),
		Ext: ".aab",
	})
	if err == nil || !strings.Contains(err.Error(), "output key") {
		t.Fatalf("err = %v", err)
	}
}
