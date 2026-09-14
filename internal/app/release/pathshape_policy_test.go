// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
)

// TestReleasePathShapes_PolicyPerConsumer runs one target path in four shapes
// through each release consumer that takes a caller-named path, and pins the
// path policy each applies:
//
//   - a named file (publish asset, release notes, exact sign file) must be a
//     regular file: a directory or FIFO is missing input, refused before the
//     provider or signer is used;
//   - an attach glob (checksums, sign) matches files, so a directory it
//     matches is skipped while a FIFO is refused as missing input;
//   - publication is confined to the workspace, so an absolute asset or notes
//     path is refused as a validation error even when it names a real file
//     inside it, while signing and checksumming accept an absolute path the
//     caller gave;
//   - a relative regular file is accepted everywhere.
//
// Symlinked leaves and parents are covered per consumer in pathpolicy_test.go.
func TestReleasePathShapes_PolicyPerConsumer(t *testing.T) {
	consumers := map[string]func(t *testing.T, path string) (int, error){
		"publish asset": func(t *testing.T, path string) (int, error) {
			t.Helper()

			prov := fakeprovider.New(t)
			err := apprelease.PublishRelease(context.Background(), prov, &bytes.Buffer{}, apprelease.PublishReleaseInput{Tag: "v1.0.0", Repository: "owner/repo", ReleaseNotesFile: "dist/notes.md", Assets: []string{path}})

			return len(prov.PublishReleaseCalls()), err
		},
		"release notes": func(t *testing.T, path string) (int, error) {
			t.Helper()

			prov := fakeprovider.New(t)
			err := apprelease.PublishRelease(context.Background(), prov, &bytes.Buffer{}, apprelease.PublishReleaseInput{Tag: "v1.0.0", Repository: "owner/repo", ReleaseNotesFile: path, Assets: []string{"dist/app.tgz"}})

			return len(prov.PublishReleaseCalls()), err
		},
		"sign file": func(_ *testing.T, path string) (int, error) {
			signer := &advertisingSigner{extensions: []string{".asc"}}
			err := apprelease.SignArtifacts(context.Background(), signer, &bytes.Buffer{}, apprelease.SignInput{Files: []string{path}, SkipReleaseArtifactsDir: true, SkipChecksumsFile: true})

			return len(signer.signed), err
		},
		"sign attach glob": func(_ *testing.T, path string) (int, error) {
			signer := &advertisingSigner{extensions: []string{".asc"}}
			err := apprelease.SignArtifacts(context.Background(), signer, &bytes.Buffer{}, apprelease.SignInput{AttachArtifacts: path, SkipReleaseArtifactsDir: true, SkipChecksumsFile: true})

			return len(signer.signed), err
		},
		"checksums attach glob": func(_ *testing.T, path string) (int, error) {
			count, err := apprelease.Checksums(&bytes.Buffer{}, apprelease.ChecksumsInput{AttachArtifacts: path, OutputFile: "dist/SHA256SUMS", ReleaseArtifactsDir: "none", SBOMDir: "none"})

			return count, err
		},
	}

	type outcome struct {
		err   error
		count int
	}

	named := map[string]outcome{"directory": {errs.ErrMissingInput, 0}, "fifo": {errs.ErrMissingInput, 0}}
	glob := map[string]outcome{"directory": {nil, 0}, "fifo": {errs.ErrMissingInput, 0}}
	publish := func(effects int) map[string]outcome {
		return map[string]outcome{"absolute": {errs.ErrValidation, 0}, "relative": {nil, effects}}
	}
	local := map[string]outcome{"absolute": {nil, 1}, "relative": {nil, 1}}

	want := map[string]map[string]outcome{
		"publish asset":         merge(named, publish(1)),
		"release notes":         merge(named, publish(1)),
		"sign file":             merge(named, local),
		"sign attach glob":      merge(glob, local),
		"checksums attach glob": merge(glob, local),
	}

	for consumer, run := range consumers {
		for _, shape := range []string{"directory", "fifo", "absolute", "relative"} {
			t.Run(consumer+"/"+shape, func(t *testing.T) {
				root := t.TempDir()
				t.Chdir(root)
				mustWrite(t, "dist/notes.md", "notes\n")
				mustWrite(t, "dist/app.tgz", "asset\n")

				path := "dist/target.tgz"

				switch shape {
				case "directory":
					if err := os.Mkdir(path, 0o755); err != nil { //nolint:gosec // owned temp fixture.
						t.Fatal(err)
					}
				case "fifo":
					if err := syscall.Mkfifo(path, 0o600); err != nil {
						t.Fatal(err)
					}
				case "absolute":
					mustWrite(t, path, "target\n")
					path = filepath.Join(root, path)
				default:
					mustWrite(t, path, "target\n")
				}

				expected := want[consumer][shape]

				effects, err := run(t, path)
				if (expected.err == nil) != (err == nil) || (expected.err != nil && !errors.Is(err, expected.err)) {
					t.Fatalf("err = %v, want %v", err, expected.err)
				}

				if effects != expected.count {
					t.Errorf("effects = %d, want %d", effects, expected.count)
				}
			})
		}
	}
}

func merge[V any](parts ...map[string]V) map[string]V {
	out := map[string]V{}

	for _, part := range parts {
		for key, value := range part {
			out[key] = value
		}
	}

	return out
}
