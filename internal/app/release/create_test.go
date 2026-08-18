// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
)

// fakeFS is an in-memory fsOps. Files map filepath → exists; Empty set
// to true for files that exist but are empty.
type fakeFS struct {
	Files             map[string]bool
	Empty             map[string]bool
	ReleaseDirHits    []string
	GlobMatches       map[string][]string
	SignatureSidecars []string
}

func (f *fakeFS) FileExists(p string) bool {
	if p == "" {
		return false
	}

	return f.Files[p]
}
func (f *fakeFS) FileNonEmpty(p string) bool {
	return f.FileExists(p) && !f.Empty[p]
}
func (f *fakeFS) FindReleaseArtifacts(dir string) []string {
	cp := make([]string, len(f.ReleaseDirHits))
	copy(cp, f.ReleaseDirHits)

	return cp
}
func (f *fakeFS) Glob(pat string) []string {
	cp := make([]string, len(f.GlobMatches[pat]))
	copy(cp, f.GlobMatches[pat])

	return cp
}
func (f *fakeFS) ListSignatureSidecars() []string {
	cp := make([]string, len(f.SignatureSidecars))
	copy(cp, f.SignatureSidecars)

	return cp
}

func TestCreateRelease_RequiresTagAndRepo(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t)

	fs := &fakeFS{Files: map[string]bool{}}
	if err := apprelease.CreateRelease(context.Background(), prov, fs, &bytes.Buffer{}, apprelease.CreateReleaseInput{
		Repository: "owner/repo", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}); err == nil || !strings.Contains(err.Error(), "tag is required") {
		t.Errorf("missing tag err = %v", err)
	}

	if err := apprelease.CreateRelease(context.Background(), prov, fs, &bytes.Buffer{}, apprelease.CreateReleaseInput{
		Tag: "v1.0.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}); err == nil || !strings.Contains(err.Error(), "REPOSITORY") {
		t.Errorf("missing repo err = %v", err)
	}
}

// TestCreateRelease_AttachesEachArtifactWithItsSignature covers a release run
// with everything present: an artifact in the release directory, an SBOM zip,
// a checksums file, and a signature beside each one.
//
// The claim is the pairing, so assets are asserted as an ordered list. The
// order is the behaviour — the collector emits each artifact immediately
// followed by its signature — and a set comparison cannot tell that apart from
// the same six names arriving in any arrangement.
func TestCreateRelease_AttachesEachArtifactWithItsSignature(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub)
	fs := &fakeFS{
		Files: map[string]bool{
			"release-notes.md":           true,
			"checksums.sha256":           true, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			"checksums.sha256.asc":       true, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			"my-app-1.0.0-sboms.zip":     true,
			"my-app-1.0.0-sboms.zip.asc": true,
		},
		ReleaseDirHits: []string{
			"./release-artifacts/my-app-1.0.0.tgz",
		},
		GlobMatches:       map[string][]string{},
		SignatureSidecars: []string{},
	}
	// Mark the .asc-from-basename probe true so collect_release_artifacts
	// adds the signature line.
	fs.Files["./release-artifacts/my-app-1.0.0.tgz"] = true
	fs.Files["my-app-1.0.0.tgz.asc"] = true

	var out bytes.Buffer

	err := apprelease.CreateRelease(context.Background(), prov, fs, &out, apprelease.CreateReleaseInput{
		Tag:          "v1.0.0",
		Repository:   "owner/my-app",
		ArtifactName: "my-app",
		MakeLatest:   "true",
	})
	if err != nil {
		t.Fatal(err)
	}

	calls := prov.CreateReleaseCalls()
	if len(calls) != 1 {
		t.Fatalf("CreateRelease calls = %d", len(calls))
	}

	got := calls[0]
	if got.Repo != "owner/my-app" || got.Spec.Tag != "v1.0.0" {
		t.Errorf("call = %+v", got)
	}

	if got.Spec.Name != "v1.0.0" {
		t.Errorf("Name should default to Tag, got %q", got.Spec.Name)
	}

	wantAssets := []string{
		"./release-artifacts/my-app-1.0.0.tgz",
		"my-app-1.0.0.tgz.asc",
		"my-app-1.0.0-sboms.zip",
		"my-app-1.0.0-sboms.zip.asc",
		"checksums.sha256",
		"checksums.sha256.asc",
	}
	if !reflect.DeepEqual(got.Spec.Assets, wantAssets) {
		t.Errorf("assets = %v\nwant %v", got.Spec.Assets, wantAssets)
	}
}

// TestCreateRelease_MarksOnlyPrereleaseTagsAsPrerelease states the rule in both
// directions. The negative case used to sit inside the full-release test, where
// a reader looking for the prerelease rule would not find it.
func TestCreateRelease_MarksOnlyPrereleaseTagsAsPrerelease(t *testing.T) {
	t.Parallel()

	for tag, want := range map[string]bool{
		"v1.0.0":      false,
		"v1.0.0-rc.1": true,
	} {
		prov := fakeprovider.New(t)

		err := apprelease.CreateRelease(context.Background(), prov, &fakeFS{Files: map[string]bool{}}, &bytes.Buffer{}, apprelease.CreateReleaseInput{
			Tag:        tag,
			Repository: "owner/repo",
		})
		if err != nil {
			t.Fatal(err)
		}

		if got := prov.CreateReleaseCalls()[0].Spec.Prerelease; got != want {
			t.Errorf("tag %q: prerelease = %v, want %v", tag, got, want)
		}
	}
}

// TestCreateRelease_AttachesGlobMatchesInSortedOrder pins the determinism the
// collector goes out of its way to provide: glob matches are sorted before
// they are attached, so the release carries the same asset list from one run
// to the next whatever order the filesystem enumerated them in.
//
// The fixture returns foo before bar for exactly that reason. A set comparison
// -- what this asserted before -- cannot see a sort at all.
func TestCreateRelease_AttachesGlobMatchesInSortedOrder(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t)
	fs := &fakeFS{
		Files: map[string]bool{
			"build/foo.zip": true, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			"build/bar.zip": true, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		GlobMatches: map[string][]string{
			"build/*.zip": {"build/foo.zip", "build/bar.zip"},
		},
	}

	err := apprelease.CreateRelease(context.Background(), prov, fs, &bytes.Buffer{}, apprelease.CreateReleaseInput{
		Tag:             "v1.0.0",
		Repository:      "owner/repo",
		AttachArtifacts: "build/*.zip",
	})
	if err != nil {
		t.Fatal(err)
	}

	wantAssets := []string{"build/bar.zip", "build/foo.zip"}
	if assets := prov.CreateReleaseCalls()[0].Spec.Assets; !reflect.DeepEqual(assets, wantAssets) {
		t.Errorf("assets = %v, want %v", assets, wantAssets)
	}
}

// TestCreateRelease_SplitsTheAttachListOnCommas covers the caller-facing shape
// of --attach-artifacts: one comma-separated list, surrounding spaces ignored,
// entries attached in the order they were written.
//
// The signature is in the expected list rather than in the test's name. It is
// not attached by the list at all -- the sidecar sweep runs at the end of
// collection and appends it, which is why it lands after both named files.
func TestCreateRelease_SplitsTheAttachListOnCommas(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t)

	fs := &fakeFS{
		Files: map[string]bool{
			"file1.txt":     true, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			"file2.md":      true, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			"file1.txt.asc": true, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		GlobMatches: map[string][]string{
			"file1.txt": {"file1.txt"},
			"file2.md":  {"file2.md"},
		},
		SignatureSidecars: []string{"file1.txt.asc"},
	}
	if err := apprelease.CreateRelease(context.Background(), prov, fs, &bytes.Buffer{}, apprelease.CreateReleaseInput{
		Tag:             "v1.0.0",
		Repository:      "owner/repo",
		AttachArtifacts: "file1.txt, file2.md",
	}); err != nil {
		t.Fatal(err)
	}

	wantAssets := []string{"file1.txt", "file2.md", "file1.txt.asc"}
	if assets := prov.CreateReleaseCalls()[0].Spec.Assets; !reflect.DeepEqual(assets, wantAssets) {
		t.Errorf("assets = %v, want %v", assets, wantAssets)
	}
}

func TestCreateRelease_DraftFlagPropagates(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t)
	fs := &fakeFS{Files: map[string]bool{}}

	err := apprelease.CreateRelease(context.Background(), prov, fs, &bytes.Buffer{}, apprelease.CreateReleaseInput{
		Tag:        "v1.0.0",
		Repository: "owner/repo",
		Draft:      true,
		MakeLatest: "false",
	})
	if err != nil {
		t.Fatal(err)
	}

	spec := prov.CreateReleaseCalls()[0].Spec
	if !spec.Draft {
		t.Error("Draft should be true")
	}

	if spec.MakeLatest != provider.MakeLatestFalse {
		t.Error("MakeLatest should be false")
	}
}

func TestCreateRelease_MakeLatestLegacyPropagates(t *testing.T) {
	t.Parallel()

	prov := fakeprovider.New(t)
	fs := &fakeFS{Files: map[string]bool{}}

	err := apprelease.CreateRelease(context.Background(), prov, fs, &bytes.Buffer{}, apprelease.CreateReleaseInput{
		Tag:        "v1.0.0",
		Repository: "owner/repo",
		MakeLatest: "legacy",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := prov.CreateReleaseCalls()[0].Spec.MakeLatest; got != provider.MakeLatestLegacy {
		t.Errorf("MakeLatest = %q, want %q", got, provider.MakeLatestLegacy)
	}
}

func TestCreateRelease_ChecksumsSkippedWhenEmpty(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t)
	fs := &fakeFS{
		Files: map[string]bool{
			"checksums.sha256":     true,
			"checksums.sha256.asc": true,
		},
		Empty: map[string]bool{
			"checksums.sha256": true, // exists but empty → skip
		},
	}

	var out bytes.Buffer
	if err := apprelease.CreateRelease(context.Background(), prov, fs, &out, apprelease.CreateReleaseInput{
		Tag:        "v1.0.0",
		Repository: "owner/repo",
	}); err != nil {
		t.Fatal(err)
	}

	for _, a := range prov.CreateReleaseCalls()[0].Spec.Assets {
		if a == "checksums.sha256" || a == "checksums.sha256.asc" {
			t.Errorf("empty checksums file should not attach: got %q", a)
		}
	}

	if !strings.Contains(out.String(), "or file is empty - skipping") {
		t.Errorf("expected skip message, got %q", out.String())
	}
}

func TestCreateRelease_DefaultsArtifactNameFromRepo(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t)
	fs := &fakeFS{
		Files: map[string]bool{
			"reponame-1.0.0-sboms.zip":     true,
			"reponame-1.0.0-sboms.zip.asc": true,
		},
	}

	var out bytes.Buffer
	if err := apprelease.CreateRelease(context.Background(), prov, fs, &out, apprelease.CreateReleaseInput{
		Tag:        "v1.0.0",
		Repository: "owner/reponame",
	}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "Adding SBOM ZIP: reponame-1.0.0-sboms.zip") {
		t.Errorf("expected sbom zip with reponame, got: %q", out.String())
	}
}

func TestCreateRelease_WarnsWhenSBOMZipMissing(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t)
	fs := &fakeFS{Files: map[string]bool{}}

	var out bytes.Buffer
	if err := apprelease.CreateRelease(context.Background(), prov, fs, &out, apprelease.CreateReleaseInput{
		Tag:          "v1.0.0",
		Repository:   "owner/repo",
		ArtifactName: "myapp", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "SBOM ZIP not found: myapp-1.0.0-sboms.zip") {
		t.Errorf("expected missing sbom warning, got: %q", out.String())
	}
}

func TestCreateRelease_NotesFilePassesThroughWhenNonEmpty(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t)

	fs := &fakeFS{Files: map[string]bool{"notes.md": true}} //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	if err := apprelease.CreateRelease(context.Background(), prov, fs, &bytes.Buffer{}, apprelease.CreateReleaseInput{
		Tag:              "v1.0.0",
		Repository:       "owner/repo",
		ReleaseNotesFile: "notes.md",
	}); err != nil {
		t.Fatal(err)
	}

	if got := prov.CreateReleaseCalls()[0].Spec.NotesFile; got != "notes.md" {
		t.Errorf("NotesFile = %q", got)
	}
}

func TestCreateRelease_SkipsMissingOrEmptyNotesFile(t *testing.T) {
	t.Parallel()

	tests := map[string]*fakeFS{
		"missing": {Files: map[string]bool{}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"empty": {
			Files: map[string]bool{"notes.md": true},
			Empty: map[string]bool{"notes.md": true},
		},
	}
	for name, fs := range tests {
		t.Run(name, func(t *testing.T) {
			prov := fakeprovider.New(t)
			if err := apprelease.CreateRelease(context.Background(), prov, fs, &bytes.Buffer{}, apprelease.CreateReleaseInput{
				Tag:              "v1.0.0",
				Repository:       "owner/repo",
				ReleaseNotesFile: "notes.md",
			}); err != nil {
				t.Fatal(err)
			}

			if got := prov.CreateReleaseCalls()[0].Spec.NotesFile; got != "" {
				t.Errorf("NotesFile = %q, want empty", got)
			}
		})
	}
}

func TestCreateRelease_PropagatesProviderError(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t).WithCreateReleaseError(fakeError("boom"))
	fs := &fakeFS{Files: map[string]bool{}}

	err := apprelease.CreateRelease(context.Background(), prov, fs, &bytes.Buffer{}, apprelease.CreateReleaseInput{
		Tag:        "v1.0.0",
		Repository: "owner/repo",
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v", err)
	}
}

type fakeError string

func (e fakeError) Error() string { return string(e) }
