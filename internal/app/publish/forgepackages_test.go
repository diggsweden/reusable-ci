// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	apppublish "github.com/diggsweden/reusable-ci/v3/internal/app/publish"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

var errNoRegistry = errors.New("no registry")

type fakeRegistryResolver struct {
	reg provider.ForgeMavenRegistry
	err error
}

func (f fakeRegistryResolver) ResolveForgeMavenRegistry() (provider.ForgeMavenRegistry, error) {
	return f.reg, f.err
}

func TestForgePackagesDeploy_BuildsMvnArgsFromResolvedRegistry(t *testing.T) {
	t.Parallel()

	mvn := &fakePublishMaven{}
	resolver := fakeRegistryResolver{reg: provider.ForgeMavenRegistry{
		ServerID: "gitlab-maven", URL: "https://gl/api/v4/projects/1/packages/maven",
		AuthScheme: provider.MavenAuthJobTokenHeader, Token: "jt",
	}}

	err := apppublish.ForgePackagesDeploy(context.Background(), mvn, resolver, io.Discard, io.Discard,
		apppublish.ForgePackagesDeployInput{CLIOpts: []string{"-B"}})
	if err != nil {
		t.Fatal(err)
	}

	if len(mvn.runs) != 1 {
		t.Fatalf("expected 1 mvn run, got %d", len(mvn.runs))
	}

	// The whole argv, in order. Joining it and looking for five substrings
	// could not see an argument that should not be there, and matched "-B"
	// inside any longer token. These are the arguments a deploy runs with.
	argv := mvn.runs[0]
	if len(argv) != 6 {
		t.Fatalf("mvn args = %q, want six", argv)
	}

	// The settings path is generated per run, so it is taken from the argv and
	// checked separately: it must be the temporary file the deploy credentials
	// were written to, not an empty string or a path from elsewhere.
	settings := argv[4]
	if !strings.HasSuffix(settings, ".xml") || !strings.Contains(filepath.Base(settings), "reusable-ci-settings-") {
		t.Errorf("--settings = %q, want the generated credentials file", settings)
	}

	want := []string{
		"-B",
		"deploy",
		"-DskipTests",
		"--settings", settings,
		"-DaltDeploymentRepository=gitlab-maven::default::https://gl/api/v4/projects/1/packages/maven",
	}
	if !reflect.DeepEqual(argv, want) {
		t.Errorf("mvn args = %q\nwant %q", argv, want)
	}
}

type fakeNPMRegistryResolver struct {
	reg provider.ForgeNPMRegistry
	err error
}

func (f fakeNPMRegistryResolver) ResolveForgeNPMRegistry() (provider.ForgeNPMRegistry, error) {
	return f.reg, f.err
}

type fakeNPMPublish struct {
	dir  string
	args []string
}

func (f *fakeNPMPublish) RunInherit(_ context.Context, dir string, _, _ io.Writer, args ...string) error {
	f.dir = dir
	f.args = args

	return nil
}

func TestForgePackagesNPMPublish_PublishesTheTarball(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pkg-1.0.0.tgz"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	npmOps := &fakeNPMPublish{}
	resolver := fakeNPMRegistryResolver{reg: provider.ForgeNPMRegistry{Registry: "https://gl/api/v4/projects/1/packages/npm/", Token: "jt"}}

	err := apppublish.ForgePackagesNPMPublish(context.Background(), npmOps, resolver, io.Discard, io.Discard,
		apppublish.ForgePackagesNPMPublishInput{WorkingDir: dir})
	if err != nil {
		t.Fatal(err)
	}

	got := strings.Join(npmOps.args, " ")
	if !strings.Contains(got, "publish") || !strings.Contains(got, "pkg-1.0.0.tgz") || !strings.Contains(got, "--userconfig") {
		t.Errorf("npm args = %v", npmOps.args)
	}

	// The tarball must be handed over as "./<name>" — npm runs with dir as its
	// working directory, so a dir-joined path resolves to dir/dir/<name>, does
	// not exist, and npm silently reparses the argument as a package spec where
	// "<dir>/<name>.tgz" is GitHub shorthand. A Contains check passes for both
	// spellings, which is why this was not caught until a real npm saw it.
	if !slices.Contains(npmOps.args, "./pkg-1.0.0.tgz") {
		t.Errorf("tarball must be passed as ./pkg-1.0.0.tgz relative to the working dir, got %v", npmOps.args)
	}

	for _, arg := range npmOps.args {
		if strings.HasSuffix(arg, ".tgz") && strings.Contains(strings.TrimPrefix(arg, "./"), "/") {
			t.Errorf("tarball argument %q carries a directory component; npm reads that as a package spec, not a file", arg)
		}
	}
}

func TestForgePackagesNPMPublish_NoTarballIsMissingInput(t *testing.T) {
	t.Parallel()

	err := apppublish.ForgePackagesNPMPublish(context.Background(), &fakeNPMPublish{},
		fakeNPMRegistryResolver{reg: provider.ForgeNPMRegistry{Registry: "https://r/"}}, io.Discard, io.Discard,
		apppublish.ForgePackagesNPMPublishInput{WorkingDir: t.TempDir()})

	if !errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("no tarball should be ErrMissingInput, got %v", err)
	}
}

func TestForgePackagesDeploy_ResolverErrorIsReturned(t *testing.T) {
	t.Parallel()

	err := apppublish.ForgePackagesDeploy(context.Background(), &fakePublishMaven{}, fakeRegistryResolver{err: errNoRegistry}, io.Discard, io.Discard, apppublish.ForgePackagesDeployInput{})

	if !errors.Is(err, errNoRegistry) {
		t.Errorf("expected resolver error to propagate, got %v", err)
	}
}
