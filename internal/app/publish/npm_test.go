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

	apppublish "github.com/diggsweden/reusable-ci/v3/internal/app/publish"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// Named so the propagation tests assert identity rather than a message.
var errNPMFailed = errors.New("npm exited non-zero")

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

func TestWriteNPMRC_RejectsEncodedPathControlsBeforeWriting(t *testing.T) {
	t.Parallel()

	for _, encoded := range []string{"%0a", "%0d", "%09", "%00", "%c2%85"} {
		t.Run(encoded, func(t *testing.T) {
			t.Parallel()

			out := bytes.NewBufferString("existing output\n")

			err := apppublish.WriteNPMRC(apppublish.NPMRCInput{
				Registry: "https://registry.example/packages/" + encoded + "extra", Output: out,
			})
			if !errors.Is(err, errs.ErrUsage) {
				t.Errorf("err = %v, want ErrUsage", err)
			}

			if out.String() != "existing output\n" {
				t.Error("rejected input changed the output")
			}
		})
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

	err := apppublish.NPMValidateTarball(context.Background(), &out, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), apppublish.NPMValidateTarballInput{Dir: fsys.Root})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation — the tarball is there and does not carry the entry point", err)
	}

	if !strings.Contains(out.String(), "✗ dist/cli.js NOT found") {
		t.Errorf("missing miss-marker:\n%s", out.String())
	}
}

func TestNPMValidateTarball_NoTarballErrors(t *testing.T) {
	fsys := testfs.NewReal(t)

	var stderr bytes.Buffer

	// ErrMissingInput, not ErrValidation: nothing was produced to validate,
	// which is an upstream build failure rather than a bad package.
	err := apppublish.NPMValidateTarball(context.Background(), io.Discard, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), apppublish.NPMValidateTarballInput{Dir: fsys.Root})
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("err = %v, want ErrMissingInput", err)
	}

	if !strings.Contains(stderr.String(), "::error::No tarball") {
		t.Errorf("expected ::error:: line, got: %s", stderr.String())
	}
}

// TestNPMValidateTarball_AcceptsATarGzSuffix pins that both spellings of
// the archive are recognised. The old name claimed .tgz was preferred over
// .tar.gz; findFirstTarball expresses no preference -- it takes the first
// entry ReadDir returns that carries either suffix -- and nothing here
// tested a preference either, so the name promised a rule that does not
// exist.
func TestNPMValidateTarball_AcceptsATarGzSuffix(t *testing.T) {
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

	if err := apppublish.NPMCheckVersion(context.Background(), fakeNPMOps{out: `{"name":"@org/app","version":"1.2.3"}`}, sink, io.Discard, output.NewAnnotator(&stderr, output.FormatGitHub), apppublish.NPMCheckVersionInput{ //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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

	err := apppublish.NPMCheckVersion(context.Background(), fakeNPMOps{stderr: "network down", err: errNPMFailed}, sink, io.Discard, output.Annotator{}, apppublish.NPMCheckVersionInput{
		Dir:     fsys.Root,
		Version: "1.2.3",
	})
	if !errors.Is(err, errNPMFailed) {
		t.Fatalf("err = %v, want npm's own failure to survive wrapping", err)
	}

	// And no answer was published: a registry that could not be reached is
	// not evidence that the version is unpublished.
	if got := sink.Keys(); len(got) != 0 {
		t.Errorf("emitted %q after a failed lookup", got)
	}
}

func TestNPMCheckVersion_DoesNotTreatGenericNotFoundAsVersionMissing(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"@org/app"}`))

	err := apppublish.NPMCheckVersion(context.Background(), fakeNPMOps{
		stderr: "registry host not found",
		err:    errNPMFailed,
	}, fakeoutputsink.New(t), io.Discard, output.Annotator{}, apppublish.NPMCheckVersionInput{
		Dir:     fsys.Root,
		Version: "1.2.3",
	})

	// "not found" in the message is not the E404 that means "no such
	// version": only the code is, so a DNS failure must still propagate.
	if !errors.Is(err, errNPMFailed) {
		t.Fatalf("err = %v, want the registry failure to propagate", err)
	}
}

func TestWriteNPMRC_WritesScopedAndUnscopedAuthLines(t *testing.T) {
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
				"@diggsweden:registry=https://npm.pkg.github.com\n",
		},
		{
			name: "unscoped public registry",
			in: apppublish.NPMRCInput{
				Registry: "https://registry.npmjs.org/",
			},
			wantBody: "//registry.npmjs.org/:_authToken=${NODE_AUTH_TOKEN}\n" +
				"registry=https://registry.npmjs.org\n",
		},
		{
			name: "registry with port + path is preserved",
			in: apppublish.NPMRCInput{
				Registry: "https://npm.example.com:8443/path",
				Scope:    "@example",
			},
			wantBody: "//npm.example.com:8443/path/:_authToken=${NODE_AUTH_TOKEN}\n" +
				"@example:registry=https://npm.example.com:8443/path\n",
		},
		{
			// http is permitted for a loopback registry (e.g. verdaccio in
			// local dev): the token can't leave the machine.
			name: "plaintext http allowed for localhost",
			in: apppublish.NPMRCInput{
				Registry: "http://localhost:4873",
			},
			wantBody: "//localhost:4873/:_authToken=${NODE_AUTH_TOKEN}\n" +
				"registry=http://localhost:4873\n",
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
			want: `scheme "ftp" must be https`,
		},
		{
			name: "plaintext http to remote host is rejected",
			in:   apppublish.NPMRCInput{Registry: "http://npm.example.com", Output: io.Discard},
			want: "would leak the npm auth token",
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

			// Every one of these is the caller handing over an unusable
			// registry or scope, so they share ErrUsage; the message is what
			// tells them apart for an operator.
			if !errors.Is(err, errs.ErrUsage) {
				t.Fatalf("err = %v, want ErrUsage", err)
			}

			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}
