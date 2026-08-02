// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import "testing"

func TestReleaseImagesGroupExposesBoundaryCommands(t *testing.T) {
	t.Parallel()

	found := map[string]bool{}
	for _, cmd := range releaseImagesGroup().Commands {
		found[cmd.Name] = true
	}

	for _, name := range []string{"sign", "promote", "rollback", "cleanup"} {
		if !found[name] {
			t.Errorf("release-images command %q not exposed", name)
		}
	}
}

func TestValidateReleaseImagesPathConfinesLedgerUnderDist(t *testing.T) {
	t.Parallel()

	if err := validateReleaseImagesPath("dist/release-images.json", "dist"); err != nil {
		t.Fatalf("valid confined path rejected: %v", err)
	}

	for _, path := range []string{
		"release-images.json",
		"dist",
		"dist/../release-images.json",
		"dist/sub/../../release-images.json",
	} {
		if err := validateReleaseImagesPath(path, "dist"); err == nil {
			t.Errorf("path %q was not rejected", path)
		}
	}
}

func TestDefaultReleaseImageRepositoryLowercasesRepo(t *testing.T) {
	t.Parallel()

	got := defaultReleaseImageRepository("codeberg.org", "Itiquette/Nanolinter")
	if want := "codeberg.org/itiquette/nanolinter"; got != want {
		t.Errorf("defaultReleaseImageRepository() = %q, want %q", got, want)
	}
}

func TestRegistryHostNormalizesServerURL(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		raw  string
		want string
	}{
		{raw: "https://codeberg.org", want: "codeberg.org"},
		{raw: "http://git.example.test/", want: "git.example.test"},
		{raw: "registry.example.test/org/repo", want: "registry.example.test"},
	} {
		got, err := registryHost(tc.raw)
		if err != nil {
			t.Fatalf("registryHost(%q): %v", tc.raw, err)
		}

		if got != tc.want {
			t.Errorf("registryHost(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}
