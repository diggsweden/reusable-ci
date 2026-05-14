// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package publish_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apppublish "github.com/diggsweden/reusable-ci/internal/app/publish"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

// makeNPMTarball writes a gzip-tar at <dir>/<name>.tgz containing the
// given file map under a single root component "package/" (npm pack
// convention). Paths in the map are joined under "package/".
func makeNPMTarball(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	for rel, body := range files {
		full := "package/" + rel
		if err := tw.WriteHeader(&tar.Header{
			Name:     full,
			Mode:     0o644,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNPMValidateTarball_HappyPath(t *testing.T) {
	fsys := testfs.NewReal(t)
	makeNPMTarball(t, fsys.Root, "demo-1.0.0.tgz", map[string]string{
		"package.json": `{"name":"demo"}`,
		"dist/cli.js":  "#!/usr/bin/env node\n",
	})

	var stdout, stderr bytes.Buffer
	if err := apppublish.NPMValidateTarball(context.Background(), &stdout, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), apppublish.NPMValidateTarballInput{Dir: fsys.Root}); err != nil {
		t.Fatalf("NPMValidateTarball: %v\nstderr: %s", err, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "✓ dist/cli.js found") {
		t.Errorf("missing success line:\n%s", out)
	}
	// Tarball was deleted.
	matches, _ := filepath.Glob(filepath.Join(fsys.Root, "*.tgz"))
	if len(matches) != 0 {
		t.Errorf("expected tarball to be deleted, got %v", matches)
	}
	// dist/cli.js was extracted.
	body := fsys.ReadFile("dist/cli.js")
	if !strings.Contains(string(body), "node") {
		t.Errorf("dist/cli.js content = %q", body)
	}
}

func TestNPMValidateTarball_FlagsCLIJSMissing(t *testing.T) {
	fsys := testfs.NewReal(t)
	makeNPMTarball(t, fsys.Root, "demo-1.0.0.tgz", map[string]string{
		"package.json":  `{"name":"demo"}`,
		"dist/index.js": "module.exports = {};",
	})
	var stdout, stderr bytes.Buffer
	if err := apppublish.NPMValidateTarball(context.Background(), &stdout, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), apppublish.NPMValidateTarballInput{Dir: fsys.Root}); err != nil {
		t.Fatalf("expected no error, the script reports but doesn't fail: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "✗ dist/cli.js NOT found") {
		t.Errorf("missing miss-marker:\n%s", stdout.String())
	}
}

func TestNPMValidateTarball_NoTarballErrors(t *testing.T) {
	fsys := testfs.NewReal(t)
	var stderr bytes.Buffer
	err := apppublish.NPMValidateTarball(context.Background(), io.Discard, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), apppublish.NPMValidateTarballInput{Dir: fsys.Root})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(stderr.String(), "::error::No tarball") {
		t.Errorf("expected ::error:: line, got: %s", stderr.String())
	}
}

func TestNPMValidateTarball_PrefersTgzOverTarGz_ButAcceptsBoth(t *testing.T) {
	// Just confirms .tar.gz is a recognised suffix as well.
	fsys := testfs.NewReal(t)
	makeNPMTarball(t, fsys.Root, "demo.tar.gz", map[string]string{
		"package.json": `{}`,
		"dist/cli.js":  "ok",
	})
	if err := apppublish.NPMValidateTarball(context.Background(), io.Discard, io.Discard, output.Annotator{}, apppublish.NPMValidateTarballInput{Dir: fsys.Root}); err != nil {
		t.Fatalf("NPMValidateTarball: %v", err)
	}
}
