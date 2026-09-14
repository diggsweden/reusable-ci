// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
)

// TestImmutableToolPin_OneReleaseOrNothing pins the declared-version policy.
// mise resolves anything that is not an exact release when the install runs, so
// a config that looks pinned and an install that picks up whatever was newest
// that morning are the same config. Every spelling below that mise would resolve
// has to be refused; the two spellings a registry legitimately hands back have
// to keep working, or the policy just breaks real pins.
func TestImmutableToolPin_OneReleaseOrNothing(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"1.2.3", "v1.2.3", "0.0.0", "999.999.999",
		"1.2.3-rc.1", "v1.2.3-rc.1", "1.2.3-beta1", "1.2.3+build.5", "1.2.3-rc.1+build.5",
	} {
		require.Truef(t, immutableToolPinValue(value), "%q names exactly one release", value)
	}

	for _, value := range []string{
		"latest", "lts", "stable", "system", "ref:main", "prefix:1.2", "sub-1:latest",
		"path:/opt/go", "1.2", "1", "1.2.x", "^1.2.3", "~1.2.3", ">=1.2.3", "1.2.3 || 1.2.4",
		"", " ", "1.2.3 ", " 1.2.3", "v", "vv1.2.3", "V1.2.3", "1.2.3-", "1.2.3+",
		"1.2.3-rc/1", "1.2.3-rc 1", "1.2.3\n", "1.2.3\x00", "01.2.3", "1.2.3.4",
	} {
		require.Falsef(t, immutableToolPinValue(value), "%q is resolved at install time, not pinned", value)
	}
}

// TestExactDownloadVersion_IsTheStricterSpelling keeps the two halves of the one
// policy distinguishable: a value interpolated into a release URL carries no tag
// prefix and no suffix, because the URL does not.
func TestExactDownloadVersion_IsTheStricterSpelling(t *testing.T) {
	t.Parallel()

	require.True(t, exactDownloadVersion("1.2.3"))

	for _, value := range []string{"v1.2.3", "1.2.3-rc.1", "1.2.3+build.5", "1.2", "latest", ""} {
		require.Falsef(t, exactDownloadVersion(value), "%q must not reach a download URL", value)
	}
}

// TestDeclaredToolPins_RefuseBeforeAnyEffect drives the policy through the
// configuration reader every install path starts from, in each shape mise
// accepts a version in. A mutable pin has to stop the run while it is still
// only a file being read.
func TestDeclaredToolPins_RefuseBeforeAnyEffect(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		body    string
		refused bool
	}{
		{name: "exact string", body: "[tools]\ngo = \"1.26.6\"\n"},
		{name: "tag spelling", body: "[tools]\n'aqua:rvben/rumdl' = \"v0.2.58\"\n"},
		{name: "table with a version", body: "[tools]\n'aqua:org/tool' = {version = \"1.2.3\"}\n"},
		{name: "list of exact versions", body: "[tools]\npython = [\"3.12.0\", \"3.13.1\"]\n"},
		{name: "alias", body: "[tools]\ngo = \"latest\"\n", refused: true},
		{name: "partial version", body: "[tools]\ngo = \"1.26\"\n", refused: true},
		{name: "range", body: "[tools]\ngo = \"^1.26.0\"\n", refused: true},
		{name: "ref selector", body: "[tools]\n'ubi:org/tool' = \"ref:main\"\n", refused: true},
		{name: "table without a version", body: "[tools]\n'aqua:org/tool' = {backend = \"aqua\"}\n", refused: true},
		{name: "table with a mutable version", body: "[tools]\n'aqua:org/tool' = {version = \"latest\"}\n", refused: true},
		{name: "list with one mutable entry", body: "[tools]\npython = [\"3.12.0\", \"latest\"]\n", refused: true},
		{name: "non-string version", body: "[tools]\ngo = 126\n", refused: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writePinFixture(t, root, ".mise.toml", testCase.body)

			_, _, err := readMiseConfigs(root)
			if !testCase.refused {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, errs.ErrValidation)
			require.ErrorContains(t, err, ".mise.toml declares")
		})
	}
}

// TestMiseLockPins_AreCheckedWhenTheyParse covers the lockfile half. A lock is
// the record of what was installed, so a resolvable spelling in it is a lock
// that does not lock. Its format is mise's to define, though, so one this
// cannot parse is left alone rather than refused on a guess.
func TestMiseLockPins_AreCheckedWhenTheyParse(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		body    string
		refused bool
	}{
		{name: "exact", body: "[tools]\ngo = \"1.26.6\"\n"},
		{name: "table form", body: "[tools.go]\nversion = \"1.26.6\"\nbackend = \"core:go\"\n"},
		{name: "mutable", body: "[tools]\ngo = \"latest\"\n", refused: true},
		{name: "unparsable is mise's contract", body: "locked\n"},
		{name: "no tools table", body: "# owned lock fixture\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writePinFixture(t, root, ".mise.toml", "[tools]\ngo = \"1.26.6\"\n")
			writePinFixture(t, root, "mise.lock", testCase.body)

			_, _, err := readMiseConfigs(root)
			if !testCase.refused {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, errs.ErrValidation)
			require.ErrorContains(t, err, "mise.lock declares")
		})
	}
}

// writePinFixture writes one owned configuration file for these tests. The
// package's other fixture helper lives in the external test package, which
// cannot reach the unexported policy under test here.
func writePinFixture(t *testing.T, root, rel, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte(body), 0o600))
}

// TestRepositoryToolPinsSatisfyThePolicy applies the policy to this repository's
// own declarations. A rule the repo it ships in does not follow is a rule that
// will be relaxed the first time it fails.
func TestRepositoryToolPinsSatisfyThePolicy(t *testing.T) {
	t.Parallel()

	root := ".."
	for range 3 {
		if _, err := readToolchainFile(root, ".mise.toml"); err == nil {
			break
		}

		root = filepath.Join("..", root)
	}

	_, hasConfig, err := readMiseConfigs(root)
	require.NoError(t, err, "this repository's own tool declarations must satisfy the pin policy")
	require.True(t, hasConfig, "no .mise.toml found from %s", root)
}
