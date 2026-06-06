// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package deps_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/internal/testutil/testenv"
)

func TestBuild_DetectsPlatformAndWiresDependencies(t *testing.T) {
	tests := []struct {
		name   string
		github bool
		gitlab bool
		want   provider.Platform
	}{
		{name: "github", github: true, want: provider.PlatformGitHub},
		{name: "gitlab", gitlab: true, want: provider.PlatformGitLab},
		{name: "local", want: provider.PlatformLocal},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			env := testenv.New(t)
			env.Setenv("GITHUB_ACTIONS", boolEnv(testCase.github))
			env.Setenv("GITLAB_CI", boolEnv(testCase.gitlab))

			d, err := deps.Build(context.Background()) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			if err != nil {
				t.Fatal(err)
			}

			if d.Platform != testCase.want {
				t.Errorf("Platform = %q, want %q", d.Platform, testCase.want)
			}

			if d.Provider == nil {
				t.Fatal("Provider nil")
			}

			if got := d.Provider.Name(); got != testCase.want {
				t.Errorf("Provider.Name = %q, want %q", got, testCase.want)
			}

			if d.OutputSink == nil {
				t.Errorf("OutputSink nil")
			}
		})
	}
}

func TestWithFormat_ClosesOutputSink(t *testing.T) {
	env := testenv.New(t)
	dir := env.MkdirAll("deps-with")
	outPath := filepath.Join(dir, "output")
	env.Setenv("GITHUB_OUTPUT", outPath)
	env.Setenv("GITHUB_ACTIONS", "true")

	if err := deps.WithFormat(context.Background(), output.FormatAuto, nil, func(d *deps.Deps) error {
		return d.OutputSink.Set(context.Background(), "key", "value")
	}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(outPath) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}

	if got := string(body); got != "key=value\n" {
		t.Errorf("output = %q", got)
	}
}

func TestBuild_GitLabWiresDotenvOutputSink(t *testing.T) {
	env := testenv.New(t)
	dir := env.MkdirAll("gitlab-output")
	outPath := filepath.Join(dir, "output.env")

	env.Setenv("GITLAB_CI", "true")
	env.Setenv("CI_OUTPUT", outPath)

	d, err := deps.Build(context.Background()) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatal(err)
	}

	if setErr := d.OutputSink.Set(context.Background(), "version-no-v", "1.2.3"); setErr != nil {
		t.Fatal(setErr)
	}

	if closeErr := d.Close(context.Background()); closeErr != nil {
		t.Fatal(closeErr)
	}

	body, err := os.ReadFile(outPath) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}

	if got := string(body); got != "VERSION_NO_V=1.2.3\n" {
		t.Errorf("output = %q", got)
	}
}

func TestBuild_WiresProviderSpecificSummarySink(t *testing.T) {
	tests := []struct {
		name    string
		github  bool
		gitlab  bool
		want    string
		unwants []string
	}{
		{name: "github", github: true, want: "github.md", unwants: []string{"gitlab.md"}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{name: "gitlab", gitlab: true, want: "gitlab.md", unwants: []string{"github.md"}},
		{name: "local", unwants: []string{"github.md", "gitlab.md"}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			env := testenv.New(t)
			dir := env.MkdirAll("summary")
			env.Setenv("GITHUB_ACTIONS", boolEnv(testCase.github))
			env.Setenv("GITLAB_CI", boolEnv(testCase.gitlab))
			env.Setenv("GITHUB_STEP_SUMMARY", filepath.Join(dir, "github.md"))
			env.Setenv("CI_SUMMARY_FILE", filepath.Join(dir, "gitlab.md"))

			d, err := deps.Build(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			if err := d.SummarySink.Append(context.Background(), "summary\n"); err != nil {
				t.Fatal(err)
			}

			if testCase.want != "" {
				body, err := os.ReadFile(filepath.Join(dir, testCase.want)) //nolint:gosec // test fixture
				if err != nil {
					t.Fatal(err)
				}

				if got := string(body); got != "summary\n" {
					t.Errorf("summary = %q", got)
				}
			}

			for _, unwanted := range testCase.unwants {
				if _, err := os.Stat(filepath.Join(dir, unwanted)); err == nil {
					t.Errorf("%s was written unexpectedly", unwanted)
				}
			}
		})
	}
}

func TestBuild_LocalOutputSinkIgnoresCIOutputPaths(t *testing.T) {
	env := testenv.New(t)
	dir := env.MkdirAll("local-output")
	githubOutput := filepath.Join(dir, "github-output")
	ciOutput := filepath.Join(dir, "ci-output")

	env.Setenv("GITHUB_ACTIONS", "")
	env.Setenv("GITLAB_CI", "")
	env.Setenv("GITHUB_OUTPUT", githubOutput)
	env.Setenv("CI_OUTPUT", ciOutput)

	d, err := deps.Build(context.Background()) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatal(err)
	}

	if err := d.OutputSink.Set(context.Background(), "key", "value"); err != nil {
		t.Fatal(err)
	}

	if err := d.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{githubOutput, ciOutput} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s was written unexpectedly", filepath.Base(path))
		}
	}
}

func boolEnv(b bool) string {
	if b {
		return "true"
	}

	return ""
}

// TestRequireRoles_LocalReturnsTypedErrors pins the LSP design: on
// local platform, the role-gated accessors surface a typed error
// instead of returning a stub that fails deeper in the use case.
func TestRequireRoles_LocalReturnsTypedErrors(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("GITHUB_ACTIONS", "")
	env.Setenv("GITLAB_CI", "")

	d, err := deps.Build(context.Background()) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatal(err)
	}

	if d.Platform != provider.PlatformLocal {
		t.Fatalf("Platform = %q, want local", d.Platform)
	}

	if _, err := d.RequireTokenValidator(); !errors.Is(err, errs.ErrUnsupported) {
		t.Errorf("RequireTokenValidator: err = %v, want ErrUnsupported", err)
	}

	if _, err := d.RequireReleaseCreator(); !errors.Is(err, errs.ErrUnsupported) {
		t.Errorf("RequireReleaseCreator: err = %v, want ErrUnsupported", err)
	}

	if _, err := d.RequireReleaseAssetUploader(); !errors.Is(err, errs.ErrUnsupported) {
		t.Errorf("RequireReleaseAssetUploader: err = %v, want ErrUnsupported", err)
	}

	if _, err := d.RequireSARIFUploader(); !errors.Is(err, errs.ErrUnsupported) {
		t.Errorf("RequireSARIFUploader: err = %v, want ErrUnsupported", err)
	}

	// RepoMetadataFetcher is always available — local returns empty.
	if d.RepoMetadataFetcher() == nil {
		t.Error("RepoMetadataFetcher() returned nil on local")
	}
}

// TestRequireRoles_GitHubSatisfiesEveryRole pins github.Provider's
// role conformance — every role accessor must succeed when running
// under GitHub Actions.
func TestRequireRoles_GitHubSatisfiesEveryRole(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("GITHUB_ACTIONS", "true")
	env.Setenv("GITLAB_CI", "")

	d, err := deps.Build(context.Background()) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatal(err)
	}

	if _, err := d.RequireTokenValidator(); err != nil {
		t.Errorf("RequireTokenValidator: %v", err)
	}

	if _, err := d.RequireReleaseCreator(); err != nil {
		t.Errorf("RequireReleaseCreator: %v", err)
	}

	if _, err := d.RequireReleaseAssetUploader(); err != nil {
		t.Errorf("RequireReleaseAssetUploader: %v", err)
	}

	if _, err := d.RequireSARIFUploader(); err != nil {
		t.Errorf("RequireSARIFUploader: %v", err)
	}
}

// TestRequireRoles_GitLabHasNoSARIFOrAssetUpload pins gitlab.Provider's
// role conformance — SARIFUploader and ReleaseAssetUploader are
// deliberately unimplemented.
func TestRequireRoles_GitLabHasNoSARIFOrAssetUpload(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("GITHUB_ACTIONS", "")
	env.Setenv("GITLAB_CI", "true")

	d, err := deps.Build(context.Background()) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatal(err)
	}

	if _, err := d.RequireTokenValidator(); err != nil {
		t.Errorf("RequireTokenValidator: %v", err)
	}

	if _, err := d.RequireReleaseCreator(); err != nil {
		t.Errorf("RequireReleaseCreator: %v", err)
	}

	if _, err := d.RequireReleaseAssetUploader(); !errors.Is(err, errs.ErrUnsupported) {
		t.Errorf("RequireReleaseAssetUploader: err = %v, want ErrUnsupported", err)
	}

	if _, err := d.RequireSARIFUploader(); !errors.Is(err, errs.ErrUnsupported) {
		t.Errorf("RequireSARIFUploader: err = %v, want ErrUnsupported", err)
	}
}
