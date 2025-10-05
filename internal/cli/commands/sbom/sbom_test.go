// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"context"
	"errors"
	"github.com/urfave/cli/v3"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	sbomcmd "github.com/diggsweden/reusable-ci/v3/internal/cli/commands/sbom"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestAssembleCmd_InvalidProjectTypeFailsFast(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	cmd := sbomcmd.New()

	err := cmd.Run(context.Background(), []string{"sbom", "assemble", "--project-type", "unknown"})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	if !strings.Contains(err.Error(), `invalid --project-type "unknown"`) {
		t.Errorf("err = %v, want it to quote the rejected value", err)
	}

	if !strings.Contains(err.Error(), "valid:") {
		t.Errorf("expected valid-type list, got: %v", err)
	}
}

func TestBuildCmd_UnsupportedEcoIsUsageError(t *testing.T) {
	cmd := sbomcmd.New()

	// maven's build BOM is a build byproduct, not a standalone generate.
	err := cmd.Run(context.Background(), []string{"sbom", "build", "--project-type", "maven"})
	// The name is the claim: this is misuse of the verb, not a missing tool.
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	if !strings.Contains(err.Error(), "byproduct of `build maven run`") {
		t.Errorf("err = %v, want it to name the verb that does produce the BOM", err)
	}
}

func TestBuildCmd_GoGeneratesBuildBOM(t *testing.T) {
	mock := mockbinary.New(t)
	// cyclonedx-gomod ... -output <dir>/bom.json . — create the output file.
	mock.Add("cyclonedx-gomod", `
for ((i=1; i<=$#; i++)); do
  if [[ "${!i}" == "-output" ]]; then
    next=$((i+1)); printf '{}' > "${!next}"
  fi
done
`)

	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.Chdir()

	cmd := sbomcmd.New()
	if err := cmd.Run(context.Background(), []string{"sbom", "build", "--project-type", "go", "--name", "myapp"}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".reusable-ci", "go-build-sbom", "myapp", "bom.json")); err != nil {
		t.Errorf("expected generated bom.json: %v", err)
	}
}

func TestAssembleContainerMode_RequiresRefNameFromEnv(t *testing.T) {
	env := testenv.New(t)
	// IMAGE_* selects assemble's container mode; missing ref name then errors.
	env.Setenv("IMAGE_NAME", "registry/app")
	env.Setenv("IMAGE_DIGEST", "sha256:abc")
	env.Setenv("CI_REF_NAME", "")
	env.Setenv("GITHUB_REF_NAME", "")
	env.Setenv("FORGEJO_REF_NAME", "")

	cmd := sbomcmd.New()

	err := cmd.Run(context.Background(), []string{"sbom", "assemble"})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	if !strings.Contains(err.Error(), "ref name is required") {
		t.Errorf("err = %v, want it to name the missing input", err)
	}
}

func TestAssembleContainerMode_UsesGitHubEnvInputs(t *testing.T) {
	mock := mockbinary.New(t)
	mock.Add("syft", `
for ((i=1; i<=$#; i++)); do
  if [[ "${!i}" == "-o" ]]; then
    next=$((i+1))
    output="${!next}"
    printf '{}' > "${output#*=}"
  fi
done
`)

	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.Chdir()

	env := testenv.New(t)
	env.Setenv("CI_REF_NAME", "")
	env.Setenv("CI_REPO", "")
	env.Setenv("GITHUB_REF_NAME", "v2.5.3")
	env.Setenv("GITHUB_REPOSITORY", "my-org/my-service")
	env.Setenv("ARTIFACT_TYPES", "maven")
	env.Setenv("IMAGE_NAME", "registry.example.com/team/app")
	env.Setenv("IMAGE_DIGEST", "sha256:"+strings.Repeat("d", 64))

	cmd := sbomcmd.New()
	if err := cmd.Run(context.Background(), []string{"sbom", "assemble"}); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"my-service-2.5.3-analyzed-container-sbom.spdx.json",
		"my-service-2.5.3-analyzed-container-sbom.cyclonedx.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}

	calls := mock.Invocations("syft")
	if len(calls) != 1 {
		t.Fatalf("expected 1 syft call, got %d", len(calls))
	}

	// The whole argv: the digest-pinned image and one sorted output per format,
	// named from the GitHub repository and the v-stripped ref, written in the
	// working directory. The digest used to be the non-canonical
	// "sha256:deadbeef0123", which the command now refuses, so this test had
	// been failing before it reached syft.
	wantArgs := []string{
		"registry.example.com/team/app@sha256:" + strings.Repeat("d", 64),
		"-o", "cyclonedx-json=my-service-2.5.3-analyzed-container-sbom.cyclonedx.json",
		"-o", "spdx-json=my-service-2.5.3-analyzed-container-sbom.spdx.json",
	}
	if !slices.Equal(calls[0].Args, wantArgs) {
		t.Errorf("syft argv = %q, want %q", calls[0].Args, wantArgs)
	}

	if wantCwd, _ := filepath.EvalSymlinks(dir); calls[0].Cwd != wantCwd && calls[0].Cwd != dir {
		t.Errorf("syft ran in %q, want %q", calls[0].Cwd, dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Errorf("working directory holds %v (err %v), want only the two SBOMs", entries, err)
	}
}

// TestAssembleContainerMode_RefusesANonCanonicalDigestBeforeSyft: a short or
// uppercase digest cannot pin an image, so the command refuses it as a usage
// error without running the scanner.
func TestAssembleContainerMode_RefusesANonCanonicalDigestBeforeSyft(t *testing.T) {
	mock := mockbinary.New(t)
	mock.Add("syft", "exit 0")

	fsys := testfs.NewReal(t)
	fsys.Chdir()

	for _, digest := range []string{"sha256:deadbeef0123", "sha256:" + strings.Repeat("D", 64), strings.Repeat("d", 64)} {
		env := testenv.New(t)
		env.Setenv("GITHUB_REF_NAME", "v2.5.3")
		env.Setenv("GITHUB_REPOSITORY", "my-org/my-service")
		env.Setenv("IMAGE_NAME", "registry.example.com/team/app")
		env.Setenv("IMAGE_DIGEST", digest)

		err := sbomcmd.New().Run(context.Background(), []string{"sbom", "assemble"})
		if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "digest") {
			t.Errorf("digest %q: err = %v, want a usage error naming the digest", digest, err)
		}
	}

	if calls := mock.Invocations("syft"); len(calls) != 0 {
		t.Errorf("syft ran %d time(s) for refused digests", len(calls))
	}
}

func TestAssembleCmd_InvalidSignMethodIsUsageError(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	cmd := sbomcmd.New()

	// The shared spelling and the verb's former alias reach the same check.
	for _, spelling := range []string{"--method", "--sign-method"} {
		err := cmd.Run(context.Background(), []string{"sbom", "assemble", "--sign", spelling, "bogus"})
		if !errors.Is(err, errs.ErrUsage) {
			t.Fatalf("%s: err = %v, want ErrUsage", spelling, err)
		}

		if !strings.Contains(err.Error(), `--method must be "sigstore" or "kms"`) {
			t.Errorf("%s: err = %v, want it to list the accepted methods", spelling, err)
		}
	}
}

// --sign reads the self-hosted Sigstore endpoints through signflags.ReadEndpoints,
// so the verb must declare them like every other signer: an undeclared flag reads
// back as "" and SIGN_FULCIO_URL would be silently ignored.
func TestAssembleCmd_DeclaresSigstoreEndpointFlags(t *testing.T) {
	var assemble *cli.Command

	for _, sub := range sbomcmd.New().Commands {
		if sub.Name == "assemble" {
			assemble = sub
		}
	}

	if assemble == nil {
		t.Fatal("sbom assemble not found")
	}

	declared := map[string]bool{}

	for _, flag := range assemble.Flags {
		for _, name := range flag.Names() {
			declared[name] = true
		}
	}

	for _, name := range []string{"method", "sign-method", "key", "sign-key", "oidc-issuer", "fulcio-url", "rekor-url", "trusted-root"} {
		if !declared[name] {
			t.Errorf("sbom assemble does not declare --%s", name)
		}
	}
}
