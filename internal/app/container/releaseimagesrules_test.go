// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import "testing"

func TestValidateReleaseImagesPathConfinesLedgerUnderDist(t *testing.T) {
	t.Parallel()

	if err := ValidateReleaseImagesPath("dist/release-images.json", "dist"); err != nil {
		t.Fatalf("valid confined path rejected: %v", err)
	}

	for _, path := range []string{
		"release-images.json",
		"dist",
		"dist/../release-images.json",
		"dist/sub/../../release-images.json",
		// Refused since the '..' detection moved to pathsafe.Relative --
		// deliberate tightenings, pinned so they stay refused: control
		// characters have no legitimate producer in a CI flag value, and a
		// doubled slash makes the under-dist suffix read as absolute.
		"dist/release\timages.json",
		"dist/release-images.json\n",
		"dist//release-images.json",
	} {
		if err := ValidateReleaseImagesPath(path, "dist"); err == nil {
			t.Errorf("path %q was not rejected", path)
		}
	}
}

func TestDefaultReleaseImageRepositoryLowercasesRepo(t *testing.T) {
	t.Parallel()

	got := DefaultReleaseImageRepository("codeberg.org", "Itiquette/Nanolinter")
	if want := "codeberg.org/itiquette/nanolinter"; got != want {
		t.Errorf("DefaultReleaseImageRepository() = %q, want %q", got, want)
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
		got, err := RegistryHost(tc.raw)
		if err != nil {
			t.Fatalf("RegistryHost(%q): %v", tc.raw, err)
		}

		if got != tc.want {
			t.Errorf("RegistryHost(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}
