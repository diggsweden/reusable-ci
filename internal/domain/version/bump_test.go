// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/version"
)

func TestUpdateOrAddProperty_Updates(t *testing.T) {
	t.Parallel()

	body := "ignore=me\nversionName=1.0.0\nfoo=bar\n"

	got, res := version.UpdateOrAddProperty(body, "versionName", "1.2.3", "=")
	if !strings.Contains(got, "versionName=1.2.3") || strings.Contains(got, "versionName=1.0.0") {
		t.Errorf("update failed:\n%s", got)
	}

	if res != version.UpdatePropertyUpdated {
		t.Errorf("result = %v, want Updated", res)
	}
}

func TestUpdateCargoVersion_EncodesVersionString(t *testing.T) {
	t.Parallel()

	for _, ver := range []string{`2.0.0" # injected`, `quote'and"triple"""`, `release-\path\n`, "2.0.0\nINJECTED=true", "2.0.0\r\nINJECTED=true", "2.0.0\x00\a\v\t\x7f", "2.0.0+\u00e5"} {
		t.Run(ver, func(t *testing.T) {
			const (
				prefix = "[package]\nversion = \"child-canary\"\n\n[workspace.package]\n"
				suffix = "\nkeep = true\n\n[dependencies]\nserde = \"1\"\n"
			)

			got, section, err := version.UpdateCargoVersion(prefix+"version = \"1.0.0\""+suffix, ver)
			require.NoError(t, err)
			require.Equal(t, version.CargoSectionWorkspacePackage, section)
			require.True(t, strings.HasPrefix(got, prefix))
			require.True(t, strings.HasSuffix(got, suffix))

			var parsed map[string]any
			require.NoError(t, toml.Unmarshal([]byte(got), &parsed))
			require.Equal(t, map[string]any{
				"package":      map[string]any{"version": "child-canary"},
				"workspace":    map[string]any{"package": map[string]any{"version": ver, "keep": true}},
				"dependencies": map[string]any{"serde": "1"},
			}, parsed)
		})
	}
}

func TestUpdateCargoVersion_RefusesInvalidUTF8(t *testing.T) {
	t.Parallel()

	const body = "[package]\nversion = \"1.0.0\"\n"

	got, _, err := version.UpdateCargoVersion(body, "2.0.0\xff")
	require.ErrorIs(t, err, errs.ErrValidation)
	require.Equal(t, body, got)
}

func TestUpdateOrAddProperty_DoesNotRewriteAKeyPrefix(t *testing.T) {
	t.Parallel()

	body := "MARKETING_VERSION_OTHER = 9\n"

	got, res := version.UpdateOrAddProperty(body, "MARKETING_VERSION", "1.2.3", " = ")
	if want := "MARKETING_VERSION_OTHER = 9\nMARKETING_VERSION = 1.2.3\n"; got != want {
		t.Errorf("result = %q, want %q", got, want)
	}

	if res != version.UpdatePropertyAdded {
		t.Errorf("result = %v, want Added", res)
	}
}

func TestUpdateOrAddProperty_AppendsWhenAbsent(t *testing.T) {
	t.Parallel()

	body := "foo=bar\n"

	got, res := version.UpdateOrAddProperty(body, "version", "1.2.3", "=")
	if !strings.HasSuffix(got, "version=1.2.3\n") {
		t.Errorf("expected appended line, got:\n%q", got)
	}

	if res != version.UpdatePropertyAdded {
		t.Errorf("result = %v, want Added", res)
	}
}

func TestUpdateOrAddProperty_AppendsToFileWithoutTrailingNewline(t *testing.T) {
	t.Parallel()

	body := "foo=bar"

	got, _ := version.UpdateOrAddProperty(body, "version", "1.2.3", "=")
	if got != "foo=bar\nversion=1.2.3\n" {
		t.Errorf("got %q", got)
	}
}

func TestIncrementVersionCode_Increments(t *testing.T) {
	t.Parallel()

	body := "versionName=1.2.3\nversionCode=42\n"

	res := version.IncrementVersionCode(body)
	if res.Old != 42 || res.New != 43 {
		t.Errorf("res = %+v, want old=42 new=43", res)
	}

	if !strings.Contains(res.Body, "versionCode=43") {
		t.Errorf("body missing new versionCode:\n%s", res.Body)
	}
}

func TestIncrementVersionCode_AppendsOneWhenAbsent(t *testing.T) {
	t.Parallel()

	body := "versionName=1.2.3\n"

	res := version.IncrementVersionCode(body)
	if !res.Added || res.New != 1 {
		t.Errorf("res = %+v, want Added=true New=1", res)
	}

	if !strings.Contains(res.Body, "versionCode=1") {
		t.Errorf("body missing versionCode=1:\n%s", res.Body)
	}
}

func TestUpdateGradleJVMVersion_RewritesAnchoredVersion(t *testing.T) {
	t.Parallel()

	body := "versionName=ignore\nversion=0.1.0\nversionCode=1\n"

	got, _ := version.UpdateGradleJVMVersion(body, "1.0.0")
	if !strings.Contains(got, "version=1.0.0\n") {
		t.Errorf("expected version=1.0.0:\n%s", got)
	}

	if !strings.Contains(got, "versionName=ignore\n") {
		t.Errorf("must not touch versionName:\n%s", got)
	}
}

func TestUpdateGradleJVMVersion_AppendsWhenAbsent(t *testing.T) {
	t.Parallel()

	body := "versionName=1.2.3"

	got, res := version.UpdateGradleJVMVersion(body, "2.0.0")
	if got != "versionName=1.2.3\nversion=2.0.0\n" {
		t.Errorf("got %q", got)
	}

	if res != version.UpdatePropertyAdded {
		t.Errorf("result = %v, want Added", res)
	}
}

func TestUpdateXcodeMarketingVersion_AppendsToEmpty(t *testing.T) {
	t.Parallel()

	got, res := version.UpdateXcodeMarketingVersion("", "1.2.3")
	if got != "MARKETING_VERSION = 1.2.3\n" {
		t.Errorf("got %q", got)
	}

	if res != version.UpdatePropertyAdded {
		t.Errorf("res = %v, want Added", res)
	}
}

func TestUpdateCargoVersion_WorkspacePackagePreferred(t *testing.T) {
	t.Parallel()

	body := `[package]
name = "child"
version = "0.0.1"

[workspace.package]
version = "0.5.0"
authors = ["x"]

[dependencies]
serde = "1"
`

	got, sec, err := version.UpdateCargoVersion(body, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}

	if sec != version.CargoSectionWorkspacePackage {
		t.Errorf("section = %v, want WorkspacePackage", sec)
	}

	if !strings.Contains(got, `[workspace.package]
version = "1.0.0"`) {
		t.Errorf("workspace version not updated:\n%s", got)
	}
	// [package] version should be untouched because workspace wins.
	if !strings.Contains(got, `version = "0.0.1"`) {
		t.Errorf("[package] version was unexpectedly touched:\n%s", got)
	}
}

func TestUpdateCargoVersion_PackageWhenNoWorkspace(t *testing.T) {
	t.Parallel()

	body := `[package]
name = "demo"
version = "0.0.1"
edition = "2024"
`

	got, sec, err := version.UpdateCargoVersion(body, "0.5.9")
	if err != nil {
		t.Fatal(err)
	}

	if sec != version.CargoSectionPackage {
		t.Errorf("section = %v, want Package", sec)
	}

	if !strings.Contains(got, `version = "0.5.9"`) {
		t.Errorf("not updated:\n%s", got)
	}
}

func TestUpdateCargoVersion_ErrorsWithoutKnownSection(t *testing.T) {
	t.Parallel()

	_, _, err := version.UpdateCargoVersion(`[dependencies]
serde = "1"
`, "1.0")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if !strings.Contains(err.Error(), "[workspace.package]") {
		t.Errorf("err = %v, want it to name the sections it looked for", err)
	}
}

func TestUpdateCargoVersion_ErrorsWhenSelectedSectionHasNoVersion(t *testing.T) {
	t.Parallel()

	body := "[package]\nname = \"demo\"\n"

	got, _, err := version.UpdateCargoVersion(body, "1.2.3")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if got != body {
		t.Fatalf("manifest changed on refusal:\n%s", got)
	}
}

// TestBumpProperties_SpacedAndIndentedAssignmentsAreRewritten covers the
// spellings Java properties and xcconfig files allow and a release commit used
// to break on. `versionCode = 41` was missed by the anchored regex, then
// overwritten by the append fallback's key match as `versionCode=1`: a
// downgrade a store refuses, reported as an append. The spaced JVM version and
// an indented MARKETING_VERSION were duplicated rather than rewritten.
func TestBumpProperties_SpacedAndIndentedAssignmentsAreRewritten(t *testing.T) {
	t.Parallel()

	code := version.IncrementVersionCode("versionName = 1.4.0\nversionCode = 41\n")
	if code.Added || code.Old != 41 || code.New != 42 || code.Body != "versionName = 1.4.0\nversionCode=42\n" {
		t.Errorf("IncrementVersionCode on a spaced line = %+v, want 41 incremented to 42 in place", code)
	}

	body, result := version.UpdateGradleJVMVersion("version = 1.0.0\ngroup = se.digg\n", "1.1.0")
	if result != version.UpdatePropertyUpdated || body != "version=1.1.0\ngroup = se.digg\n" {
		t.Errorf("UpdateGradleJVMVersion on a spaced line = %q, %v; want the line rewritten", body, result)
	}

	body, result = version.UpdateXcodeMarketingVersion("  MARKETING_VERSION = 1.0\n", "1.1")
	if result != version.UpdatePropertyUpdated || body != "  MARKETING_VERSION = 1.1\n" {
		t.Errorf("UpdateXcodeMarketingVersion on an indented line = %q, %v; want the line rewritten with its indentation", body, result)
	}

	// versionName is a different property, whatever its spacing.
	body, result = version.UpdateGradleJVMVersion("versionName = 1.0.0\n", "1.1.0")
	if result != version.UpdatePropertyAdded || body != "versionName = 1.0.0\nversion=1.1.0\n" {
		t.Errorf("UpdateGradleJVMVersion beside versionName = %q, %v; want an appended version", body, result)
	}
}

// TestUpdateCargoVersion_StaysInsideTheSelectedSection: a [package] without
// a version line followed by a dependency table that has one. The rewrite
// is bounded by the next section header, so the manifest is refused rather
// than the dependency's version being bumped to the release version.
func TestUpdateCargoVersion_StaysInsideTheSelectedSection(t *testing.T) {
	t.Parallel()

	body := "[package]\nname = \"demo\"\nedition = \"2021\"\n\n[dependencies.serde]\nversion = \"1.0\"\nfeatures = [\"derive\"]\n"

	got, _, err := version.UpdateCargoVersion(body, "2.0.0")
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "no version field") {
		t.Fatalf("err = %v, want ErrValidation for a section without a version", err)
	}

	if got != body {
		t.Fatalf("the dependency version was rewritten:\n%s", got)
	}
}
