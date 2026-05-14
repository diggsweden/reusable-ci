// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package version_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/version"
)

func TestUpdateOrAddProperty_Updates(t *testing.T) {
	body := "ignore=me\nversionName=1.0.0\nfoo=bar\n"
	got, res := version.UpdateOrAddProperty(body, "versionName", "1.2.3", "=")
	if !strings.Contains(got, "versionName=1.2.3") || strings.Contains(got, "versionName=1.0.0") {
		t.Errorf("update failed:\n%s", got)
	}
	if res != version.UpdatePropertyUpdated {
		t.Errorf("result = %v, want Updated", res)
	}
}

func TestUpdateOrAddProperty_AppendsWithSeparator(t *testing.T) {
	body := "MARKETING_VERSION_OTHER = 9\n"
	got, res := version.UpdateOrAddProperty(body, "MARKETING_VERSION", "1.2.3", " = ")
	// Anchor MARKETING_VERSION_OTHER starts with MARKETING_VERSION — bash uses
	// `grep -q "^${key}"` which matches OTHER too. Mirroring the bash means we
	// rewrite the OTHER line. Confirm the actual bash semantic: `^${key}` is
	// not anchored to a word boundary, so MARKETING_VERSION_OTHER does match.
	if !strings.Contains(got, "MARKETING_VERSION = 1.2.3") {
		t.Errorf("expected key rewritten:\n%s", got)
	}
	if res != version.UpdatePropertyUpdated {
		t.Errorf("result = %v, want Updated (matched OTHER per bash semantics)", res)
	}
}

func TestUpdateOrAddProperty_AppendsWhenAbsent(t *testing.T) {
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
	body := "foo=bar"
	got, _ := version.UpdateOrAddProperty(body, "version", "1.2.3", "=")
	if got != "foo=bar\nversion=1.2.3\n" {
		t.Errorf("got %q", got)
	}
}

func TestIncrementVersionCode_Increments(t *testing.T) {
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
	got, res := version.UpdateXcodeMarketingVersion("", "1.2.3")
	if got != "MARKETING_VERSION = 1.2.3\n" {
		t.Errorf("got %q", got)
	}
	if res != version.UpdatePropertyAdded {
		t.Errorf("res = %v, want Added", res)
	}
}

func TestUpdateCargoVersion_WorkspacePackagePreferred(t *testing.T) {
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
	_, _, err := version.UpdateCargoVersion(`[dependencies]
serde = "1"
`, "1.0")
	if err == nil {
		t.Fatal("expected error")
	}
}
