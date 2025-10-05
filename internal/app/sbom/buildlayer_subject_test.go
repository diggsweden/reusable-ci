// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	appsbom "github.com/diggsweden/reusable-ci/v3/internal/app/sbom"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
	"github.com/stretchr/testify/require"
)

// TestGenerate_BuildLayerBindsToTheReleaseSubject covers what the Build layer
// asserts by naming its output after a release. Discovery walks the workspace
// for a bom.json, so in a multi-module tree the match can belong to another
// module; the bytes are then copied unchanged under a filename that says they
// describe the release. That is the binding this checks: the document has to be
// the format the name promises, and must not declare a different release.
//
// The name is deliberately not compared. Build tools name the component by
// module or artifactId while the subject carries a sanitized artifact name, and
// mapping one to the other would be a guess rather than a check.
func TestGenerate_BuildLayerBindsToTheReleaseSubject(t *testing.T) {
	for _, testCase := range []struct {
		name string
		bom  string
		err  error
	}{
		{
			name: "the release it says it is",
			bom:  `{"bomFormat":"CycloneDX","metadata":{"component":{"name":"demo","version":"1.2.3"}}}`,
		},
		{
			name: "a differently named component of the same release",
			bom:  `{"bomFormat":"CycloneDX","metadata":{"component":{"name":"internal-module","version":"1.2.3"}}}`,
		},
		{
			name: "a snapshot of the same release",
			bom:  `{"bomFormat":"CycloneDX","metadata":{"component":{"name":"demo","version":"1.2.3-SNAPSHOT"}}}`,
		},
		{
			name: "an aggregate inventory of the tree",
			bom:  `{"bomFormat":"CycloneDX","components":[{"name":"dep","version":"9.9.9"}]}`,
		},
		{
			name: "a component that states no version",
			bom:  `{"bomFormat":"CycloneDX","metadata":{"component":{"name":"demo"}}}`,
		},
		{
			name: "another module's release",
			bom:  `{"bomFormat":"CycloneDX","metadata":{"component":{"name":"other","version":"0.4.0"}}}`,
			err:  errs.ErrValidation,
		},
		{
			name: "the previous release of this module",
			bom:  `{"bomFormat":"CycloneDX","metadata":{"component":{"name":"demo","version":"1.2.2"}}}`,
			err:  errs.ErrValidation,
		},
		{
			name: "an SPDX document under a CycloneDX name",
			bom:  `{"spdxVersion":"SPDX-2.3","name":"demo"}`,
			err:  errs.ErrMalformedInput,
		},
		{
			name: "not a document at all",
			bom:  "harvested from somewhere else entirely",
			err:  errs.ErrMalformedInput,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			fsys.WriteFile("package.json", []byte(`{"name":"demo","version":"1.2.3"}`))
			fsys.WriteFile("bom.json", []byte(testCase.bom))
			fsys.Chdir()

			var out bytes.Buffer

			err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{sha: "abc1234"}, nil, &out, io.Discard, appsbom.GenerateInput{Layers: "build"})

			published := fsys.Path("demo-1.2.3-abc1234-build-sbom.cyclonedx.json")

			if testCase.err != nil {
				require.ErrorIs(t, err, testCase.err)

				_, statErr := os.Stat(published)
				require.ErrorIs(t, statErr, os.ErrNotExist, "a refused subject must not be published")

				return
			}

			require.NoError(t, err, out.String())

			body, readErr := os.ReadFile(published) //nolint:gosec // owned fixture path.
			require.NoError(t, readErr)
			require.Equal(t, testCase.bom, string(body), "the harvested bytes are copied, never rewritten")
		})
	}
}

// TestGenerate_BuildLayerRefusalNamesTheMismatch keeps the diagnostic
// actionable: which file was harvested, what it declared and what was expected.
func TestGenerate_BuildLayerRefusalNamesTheMismatch(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"demo","version":"1.2.3"}`))
	fsys.WriteFile(filepath.Join("modules", "other", "bom.json"),
		[]byte(`{"bomFormat":"CycloneDX","metadata":{"component":{"name":"other","version":"0.4.0"}}}`))
	fsys.Chdir()

	var out bytes.Buffer

	err := appsbom.Generate(context.Background(), &fakeSyft{}, nil, &fakeGit{sha: "abc1234"}, nil, &out, io.Discard, appsbom.GenerateInput{Layers: "build"})

	require.ErrorIs(t, err, errs.ErrValidation)
	require.ErrorContains(t, err, filepath.ToSlash(filepath.Join("modules", "other", "bom.json")))
	require.ErrorContains(t, err, `describes "other" version 0.4.0`)
	require.ErrorContains(t, err, "not the release being published (1.2.3)")
	require.Contains(t, out.String(), "declares version 0.4.0, not 1.2.3")
}
