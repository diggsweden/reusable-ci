// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// TestCreateRelease_LatestModesAndNames: the latest mode is case- and
// space-insensitive with true as the default, an unknown mode is a usage
// error before the forge is asked anything, and the release name is the
// explicit one or else the tag.
func TestCreateRelease_LatestModesAndNames(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		latest, name, wantName string
		want                   provider.MakeLatestMode
	}{
		{latest: "", wantName: "v1.0.0", want: provider.MakeLatestTrue},
		{latest: " TRUE ", name: "Release One", wantName: "Release One", want: provider.MakeLatestTrue},
		{latest: "false", wantName: "v1.0.0", want: provider.MakeLatestFalse},
		{latest: "Legacy", wantName: "v1.0.0", want: provider.MakeLatestLegacy},
	} {
		prov := fakeprovider.New(t)

		err := apprelease.CreateRelease(context.Background(), prov, &fakeFS{Files: map[string]bool{}}, &bytes.Buffer{}, apprelease.CreateReleaseInput{
			Tag: "v1.0.0", Repository: "owner/repo", MakeLatest: tc.latest, ReleaseName: tc.name,
		})

		calls := prov.CreateReleaseCalls()
		if err != nil || len(calls) != 1 {
			t.Fatalf("latest %q: err = %v with %d provider calls", tc.latest, err, len(calls))
		}

		if calls[0].Spec.MakeLatest != tc.want || calls[0].Spec.Name != tc.wantName {
			t.Errorf("latest %q: mode %q name %q, want %q and %q", tc.latest, calls[0].Spec.MakeLatest, calls[0].Spec.Name, tc.want, tc.wantName)
		}
	}

	prov := fakeprovider.New(t)

	err := apprelease.CreateRelease(context.Background(), prov, &fakeFS{Files: map[string]bool{}}, &bytes.Buffer{}, apprelease.CreateReleaseInput{
		Tag: "v1.0.0", Repository: "owner/repo", MakeLatest: "maybe",
	})
	if !errors.Is(err, errs.ErrUsage) || len(prov.CreateReleaseCalls()) != 0 {
		t.Errorf("latest maybe: err = %v with %d provider calls, want ErrUsage and none", err, len(prov.CreateReleaseCalls()))
	}
}

// TestCreateRelease_AssemblyPairsCosignBundlesWithTheirAssets: an asset
// travels with its own cosign bundle, a bundle whose asset the assembly does
// not list stays behind even in the same directory, and a required asset
// that is missing refuses the release before the forge is called.
//
// Not parallel: changes the working directory.
func TestCreateRelease_AssemblyPairsCosignBundlesWithTheirAssets(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	fsys.WriteFile(filepath.Join("release-files", "assets", "app.jar"), []byte("jar"))
	fsys.WriteFile(filepath.Join("release-files", "assets", "app.jar.bundle"), []byte("{}"))
	fsys.WriteFile(filepath.Join("release-files", "assets", "stray.bin.bundle"), []byte("{}"))

	writeAssembly(t, domainrelease.DefaultAssemblyFile, domainrelease.Assembly{
		Version: domainrelease.AssemblyVersion,
		Assets:  []domainrelease.AssemblyFile{{Path: "release-files/assets/app.jar", Name: "app.jar", Required: true}},
	})

	prov := fakeprovider.New(t)
	if err := apprelease.CreateRelease(context.Background(), prov, &fakeFS{Files: map[string]bool{}}, &bytes.Buffer{}, apprelease.CreateReleaseInput{
		Tag: "v1.0.0", Repository: "owner/repo", AssemblyFile: domainrelease.DefaultAssemblyFile,
	}); err != nil {
		t.Fatal(err)
	}

	want := []string{"release-files/assets/app.jar", "release-files/assets/app.jar.bundle"}
	if got := prov.CreateReleaseCalls()[0].Spec.Assets; !slices.Equal(got, want) {
		t.Errorf("assets = %v, want %v", got, want)
	}

	writeAssembly(t, domainrelease.DefaultAssemblyFile, domainrelease.Assembly{
		Version: domainrelease.AssemblyVersion,
		Assets:  []domainrelease.AssemblyFile{{Path: "release-files/assets/gone.jar", Name: "gone.jar", Required: true}},
	})

	missing := fakeprovider.New(t)

	err := apprelease.CreateRelease(context.Background(), missing, &fakeFS{Files: map[string]bool{}}, &bytes.Buffer{}, apprelease.CreateReleaseInput{
		Tag: "v1.0.0", Repository: "owner/repo", AssemblyFile: domainrelease.DefaultAssemblyFile,
	})
	if !errors.Is(err, errs.ErrMissingInput) || len(missing.CreateReleaseCalls()) != 0 {
		t.Errorf("missing asset: err = %v with %d provider calls, want ErrMissingInput and none", err, len(missing.CreateReleaseCalls()))
	}
}
