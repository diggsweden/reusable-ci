// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// TestBuildSBOMStatus_SelectsTheProjectsOwnReport lays out competing and
// decoy files for every preset in owned temporary trees. npm and Maven name
// exact paths: a dependency's bom.json under node_modules or a module's
// target/bom.json is never the project's SBOM, even when it sorts first or is
// the only one. Gradle accepts module reports but prefers the root project's
// under either report path, then the first path by name. A symlink, dangling or not, and a directory at the report path are not
// reports.
func TestBuildSBOMStatus_SelectsTheProjectsOwnReport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		ecosystem string
		files     []string
		links     map[string]string
		dirs      []string
		want      string
	}{
		{name: "npm root", ecosystem: "npm", files: []string{"bom.json", "node_modules/dep/bom.json"}, want: "bom.json"},
		{name: "npm dependency only", ecosystem: "npm", files: []string{"node_modules/dep/bom.json"}},
		{name: "maven aggregate beside a module that sorts first", ecosystem: "maven", files: []string{"api/target/bom.json", "target/bom.json"}, want: "target/bom.json"},
		{name: "maven module only", ecosystem: "maven", files: []string{"api/target/bom.json"}},
		{name: "gradle alternate path", ecosystem: "gradle", files: []string{"build/reports/cyclonedx/bom.json"}, want: "build/reports/cyclonedx/bom.json"},
		{
			name: "gradle root under the longer path beats a module", ecosystem: "gradle",
			files: []string{"app/build/reports/bom.json", "build/reports/cyclonedx/bom.json"}, want: "build/reports/cyclonedx/bom.json",
		},
		{
			name: "gradle first path at the same depth", ecosystem: "gradle-android",
			files: []string{"build/reports/cyclonedx/bom.json", "build/reports/bom.json"}, want: "build/reports/bom.json",
		},
		{
			name: "android modules only, first by name", ecosystem: "gradle-android",
			files: []string{"wear/build/reports/bom.json", "app/build/reports/cyclonedx/bom.json", "app/build/reports/bom.json"}, want: "app/build/reports/bom.json",
		},
		{name: "dangling symlink", ecosystem: "npm", links: map[string]string{"bom.json": "missing.json"}},
		{name: "symlink to another file", ecosystem: "npm", files: []string{"elsewhere.json"}, links: map[string]string{"bom.json": "elsewhere.json"}},
		{name: "gradle dangling symlink", ecosystem: "gradle", links: map[string]string{"build/reports/bom.json": "../../missing.json"}},
		{name: "directory at the report path", ecosystem: "maven", dirs: []string{"target/bom.json"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fsys := testfs.NewReal(t)
			for _, file := range tc.files {
				fsys.WriteFile(file, []byte(`{"bomFormat":"CycloneDX"}`))
			}

			for link, target := range tc.links {
				require.NoError(t, os.MkdirAll(filepath.Dir(fsys.Path(link)), 0o700))
				require.NoError(t, os.Symlink(target, fsys.Path(link)))
			}

			for _, dir := range tc.dirs {
				require.NoError(t, os.MkdirAll(fsys.Path(dir), 0o700))
			}

			sink := &fakeSummarySink{}
			require.NoError(t, appsummary.BuildSBOMStatus(t.Context(), sink, appsummary.BuildSBOMStatusInput{Ecosystem: tc.ecosystem, Outcome: "success", WorkDir: fsys.Root}))

			if tc.want == "" {
				require.NotContains(t, sink.buf.String(), "✓", sink.buf.String())
				require.Contains(t, sink.buf.String(), "- ⚠️ ")

				return
			}

			label := "CycloneDX"
			if tc.ecosystem == "maven" {
				label = "CycloneDX (aggregate)"
			}

			require.Equal(t, "### Build SBOM\n- ✓ "+label+": `"+fsys.Path(tc.want)+"`\n", sink.buf.String())
		})
	}
}

// TestBuildSBOMStatus_RefusesAnUnsupportedEcosystem: ecosystems without a
// build SBOM preset are a usage error and append nothing.
func TestBuildSBOMStatus_RefusesAnUnsupportedEcosystem(t *testing.T) {
	t.Parallel()

	for _, ecosystem := range []string{"go", "cargo", "python", "", "NPM"} {
		sink := &fakeSummarySink{}
		err := appsummary.BuildSBOMStatus(t.Context(), sink, appsummary.BuildSBOMStatusInput{Ecosystem: ecosystem, Outcome: "success", WorkDir: t.TempDir()})
		require.ErrorIs(t, err, errs.ErrUsage, ecosystem)
		require.Empty(t, sink.buf.String(), ecosystem)
	}
}

// TestBuildSBOMStatus_AnUnreadableModuleDoesNotHideTheReport: the Gradle walk
// skips a module directory it cannot read and still reports the readable one.
func TestBuildSBOMStatus_AnUnreadableModuleDoesNotHideTheReport(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("directory permissions do not restrict root")
	}

	fsys := testfs.NewReal(t)
	fsys.WriteFile("locked/build/reports/bom.json", []byte("{}"))
	fsys.WriteFile("wear/build/reports/bom.json", []byte("{}"))
	require.NoError(t, os.Chmod(fsys.Path("locked"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(fsys.Path("locked"), 0o700) }) //nolint:gosec // restores an owned temp directory so cleanup can remove it.

	sink := &fakeSummarySink{}
	require.NoError(t, appsummary.BuildSBOMStatus(t.Context(), sink, appsummary.BuildSBOMStatusInput{Ecosystem: "gradle", Outcome: "success", WorkDir: fsys.Root}))
	require.Equal(t, "### Build SBOM\n- ✓ CycloneDX: `"+fsys.Path("wear/build/reports/bom.json")+"`\n", sink.buf.String())
}
