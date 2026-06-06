// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apppublish "github.com/diggsweden/reusable-ci/internal/app/publish"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

type fakeNPMOps struct {
	out    string
	stderr string
	err    error
}

func (f fakeNPMOps) Run(context.Context, string, ...string) (string, string, error) {
	return f.out, f.stderr, f.err
}

// makeNPMTarball writes a gzip-tar at <dir>/<name>.tgz containing the
// given file map under a single root component "package/" (npm pack
// convention). Paths in the map are joined under "package/".
func makeNPMTarball(t *testing.T, dir, name string, files map[string]string) {
	t.Helper()

	path := filepath.Join(dir, name)

	out, err := os.Create(path) //nolint:gosec // test fixture path
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
}

func TestNPMValidateTarball_HappyPath(t *testing.T) {
	fsys := testfs.NewReal(t)
	makeNPMTarball(t, fsys.Root, "demo-1.0.0.tgz", map[string]string{
		"package.json": `{"name":"demo"}`, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"dist/cli.js":  "#!/usr/bin/env node\n",
	})

	var out, stderr bytes.Buffer
	if err := apppublish.NPMValidateTarball(context.Background(), &out, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), apppublish.NPMValidateTarballInput{Dir: fsys.Root}); err != nil {
		t.Fatalf("NPMValidateTarball: %v\nstderr: %s", err, stderr.String())
	}

	body := out.String()
	if !strings.Contains(body, "✓ dist/cli.js found") {
		t.Errorf("missing success line:\n%s", body)
	}
	// Tarball was deleted.
	matches, _ := filepath.Glob(filepath.Join(fsys.Root, "*.tgz"))
	if len(matches) != 0 {
		t.Errorf("expected tarball to be deleted, got %v", matches)
	}
	// dist/cli.js was extracted.
	js := fsys.ReadFile("dist/cli.js")
	if !strings.Contains(string(js), "node") {
		t.Errorf("dist/cli.js content = %q", js)
	}
}

func TestNPMValidateTarball_FlagsCLIJSMissing(t *testing.T) {
	fsys := testfs.NewReal(t)
	makeNPMTarball(t, fsys.Root, "demo-1.0.0.tgz", map[string]string{
		"package.json":  `{"name":"demo"}`,
		"dist/index.js": "module.exports = {};",
	})

	var out, stderr bytes.Buffer
	if err := apppublish.NPMValidateTarball(context.Background(), &out, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), apppublish.NPMValidateTarballInput{Dir: fsys.Root}); err == nil {
		t.Fatalf("expected missing dist/cli.js to fail")
	}

	if !strings.Contains(out.String(), "✗ dist/cli.js NOT found") {
		t.Errorf("missing miss-marker:\n%s", out.String())
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

func TestNPMCheckVersion_AlreadyPublished(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"@org/app"}`))

	sink := fakeoutputsink.New(t)

	var stderr bytes.Buffer

	if err := apppublish.NPMCheckVersion(context.Background(), fakeNPMOps{out: "1.2.3"}, sink, io.Discard, output.NewAnnotator(&stderr, output.FormatGitHub), apppublish.NPMCheckVersionInput{ //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Dir:     fsys.Root,
		Version: "1.2.3",
	}); err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("already-published"); got != "true" {
		t.Errorf("already-published = %q", got)
	}

	if !strings.Contains(stderr.String(), "already exists") {
		t.Errorf("stderr = %s", stderr.String())
	}
}

func TestNPMCheckVersion_NotFound(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"@org/app"}`))

	sink := fakeoutputsink.New(t)

	if err := apppublish.NPMCheckVersion(context.Background(), fakeNPMOps{stderr: "npm ERR! code E404", err: errors.New("not found")}, sink, io.Discard, output.Annotator{}, apppublish.NPMCheckVersionInput{ //nolint:err113 // test mock error
		Dir:     fsys.Root,
		Version: "1.2.3",
	}); err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("already-published"); got != "false" {
		t.Errorf("already-published = %q", got)
	}
}

func TestNPMCheckVersion_PropagatesUnexpectedNPMError(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"@org/app"}`))

	sink := fakeoutputsink.New(t)

	err := apppublish.NPMCheckVersion(context.Background(), fakeNPMOps{stderr: "network down", err: errors.New("npm failed")}, sink, io.Discard, output.Annotator{}, apppublish.NPMCheckVersionInput{ //nolint:err113 // test mock error
		Dir:     fsys.Root,
		Version: "1.2.3",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNPMCheckVersion_DoesNotTreatGenericNotFoundAsVersionMissing(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"@org/app"}`))

	err := apppublish.NPMCheckVersion(context.Background(), fakeNPMOps{
		stderr: "registry host not found",
		err:    errors.New("npm failed"), //nolint:err113 // test mock error
	}, fakeoutputsink.New(t), io.Discard, output.Annotator{}, apppublish.NPMCheckVersionInput{
		Dir:     fsys.Root,
		Version: "1.2.3",
	})
	if err == nil {
		t.Fatal("expected registry failure to propagate")
	}
}

func TestWriteNPMRC(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		in       apppublish.NPMRCInput
		wantBody string
	}{
		{
			name: "scoped GitHub Packages",
			in: apppublish.NPMRCInput{
				Registry: "https://npm.pkg.github.com",
				Scope:    "@diggsweden",
			},
			wantBody: "//npm.pkg.github.com/:_authToken=${NODE_AUTH_TOKEN}\n" +
				"@diggsweden:registry=https://npm.pkg.github.com\n" +
				"always-auth=true\n",
		},
		{
			name: "unscoped public registry",
			in: apppublish.NPMRCInput{
				Registry: "https://registry.npmjs.org/",
			},
			wantBody: "//registry.npmjs.org/:_authToken=${NODE_AUTH_TOKEN}\n" +
				"registry=https://registry.npmjs.org\n" +
				"always-auth=true\n",
		},
		{
			name: "registry with port + path is preserved",
			in: apppublish.NPMRCInput{
				Registry: "https://npm.example.com:8443/path",
				Scope:    "@example",
			},
			wantBody: "//npm.example.com:8443/path/:_authToken=${NODE_AUTH_TOKEN}\n" +
				"@example:registry=https://npm.example.com:8443/path\n" +
				"always-auth=true\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			tc.in.Output = &buf
			if err := apppublish.WriteNPMRC(tc.in); err != nil {
				t.Fatal(err)
			}

			if got := buf.String(); got != tc.wantBody {
				t.Errorf("body mismatch:\n got: %q\nwant: %q", got, tc.wantBody)
			}
		})
	}
}

func TestWriteNPMRC_RejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   apppublish.NPMRCInput
		want string
	}{
		{
			name: "missing registry",
			in:   apppublish.NPMRCInput{Output: io.Discard},
			want: "registry is required",
		},
		{
			name: "bad scheme",
			in:   apppublish.NPMRCInput{Registry: "ftp://example.com", Output: io.Discard},
			want: `scheme "ftp" must be http or https`,
		},
		{
			name: "missing host",
			in:   apppublish.NPMRCInput{Registry: "https:///foo", Output: io.Discard},
			want: "has no host",
		},
		{
			name: "scope without leading @",
			in:   apppublish.NPMRCInput{Registry: "https://example.com", Scope: "diggsweden", Output: io.Discard}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			want: `scope "diggsweden" is not a valid npm scope`,
		},
		{
			name: "scope with unsafe chars",
			in:   apppublish.NPMRCInput{Registry: "https://example.com", Scope: "@foo:bar", Output: io.Discard},
			want: `scope "@foo:bar" is not a valid npm scope`,
		},
		{
			name: "missing output writer",
			in:   apppublish.NPMRCInput{Registry: "https://example.com"},
			want: "output writer is required",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := apppublish.WriteNPMRC(tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}
