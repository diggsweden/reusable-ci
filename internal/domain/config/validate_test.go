// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/config"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

func TestValidate_HappyPath(t *testing.T) {
	t.Parallel()

	c := &config.Config{
		Artifacts: []config.Artifact{
			{Name: "my-lib", ProjectType: projecttype.Maven, BuildType: config.BuildTypeLibrary, PublishTo: []config.PublishTarget{config.PublishMavenCentral}},
		},
	}
	if err := config.Validate(c); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestValidate_RejectsInvalidPublishTarget(t *testing.T) {
	t.Parallel()

	c := &config.Config{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		Artifacts: []config.Artifact{
			{Name: "pkg", ProjectType: projecttype.NPM, PublishTo: []config.PublishTarget{"bogus"}},
		},
	}

	err := config.Validate(c)
	if !isValidationError(err) {
		t.Fatalf("err = %v, want ValidationError", err)
	}

	if !strings.Contains(err.Error(), "invalid publish target") {
		t.Errorf("err message: %v", err)
	}
}

func TestValidate_RejectsUnsupportedPublishTarget(t *testing.T) {
	t.Parallel()

	c := &config.Config{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		Artifacts: []config.Artifact{
			// go has no package-registry publish path at all — gradle used
			// to sit here, but gradle→github-packages is supported now.
			{Name: "go-lib", ProjectType: projecttype.Go, PublishTo: []config.PublishTarget{config.PublishGitHubPackages}},
			{Name: "maven-app", ProjectType: projecttype.Maven, BuildType: config.BuildTypeApplication, PublishTo: []config.PublishTarget{config.PublishMavenCentral}},
			{Name: "npm-public", ProjectType: projecttype.NPM, PublishTo: []config.PublishTarget{config.PublishNPMJS}},
		},
	}

	err := config.Validate(c)
	if !isValidationError(err) {
		t.Fatalf("err = %v, want ValidationError", err)
	}

	// A semantic config violation must classify as ErrInvalidConfig so the
	// exit-code ladder reports EX_CONFIG (78), not the EX_SOFTWARE default.
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Errorf("err should wrap errs.ErrInvalidConfig, got %v", err)
	}

	for _, want := range []string{"go-lib", "maven-app", "npm-public"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err should mention %q: %v", want, err)
		}
	}
}

func TestValidate_RejectsEmptyArtifacts(t *testing.T) {
	t.Parallel()

	err := config.Validate(&config.Config{})
	if !isValidationError(err) {
		t.Fatalf("err = %v, want ValidationError", err)
	}

	if !strings.Contains(err.Error(), "no artifacts") {
		t.Errorf("err message: %v", err)
	}
}

func TestValidate_RejectsInvalidProjectType(t *testing.T) {
	t.Parallel()

	c := &config.Config{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		Artifacts: []config.Artifact{
			{Name: "x", ProjectType: "rust"}, // not in ValidProjectTypes
		},
	}

	err := config.Validate(c)
	if !isValidationError(err) {
		t.Fatalf("err = %v, want ValidationError", err)
	}

	if !strings.Contains(err.Error(), "invalid projectType") {
		t.Errorf("err message: %v", err)
	}
}

func TestValidate_AcceptsAllKnownTypes(t *testing.T) {
	t.Parallel()

	for _, pt := range config.ValidProjectTypes {
		t.Run(string(pt), func(t *testing.T) {
			t.Parallel()

			artifact := config.Artifact{Name: "a", ProjectType: pt}
			if pt == projecttype.Go {
				artifact.Go = &config.GoConfig{BuildMode: config.GoBuildModeArtifactFirst}
			}

			if pt == projecttype.Cargo {
				artifact.Cargo = &config.CargoConfig{BuildMode: config.CargoBuildModeContainerFirst}
			}

			c := &config.Config{
				Artifacts: []config.Artifact{artifact},
			}
			if err := config.Validate(c); err != nil {
				t.Errorf("project-type %q rejected: %v", pt, err)
			}
		})
	}
}

func TestValidate_GoRequiresBuildMode(t *testing.T) {
	t.Parallel()

	c := &config.Config{Artifacts: []config.Artifact{{Name: "go-app", ProjectType: projecttype.Go}}}

	err := config.Validate(c)
	if !isValidationError(err) {
		t.Fatalf("err = %v, want ValidationError", err)
	}

	if !strings.Contains(err.Error(), "requires config.build-mode") {
		t.Errorf("err message: %v", err)
	}
}

func TestValidate_GoRejectsInvalidBuildMode(t *testing.T) {
	t.Parallel()

	c := &config.Config{Artifacts: []config.Artifact{{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		Name:        "go-app",
		ProjectType: projecttype.Go,
		Go:          &config.GoConfig{BuildMode: "native"},
	}}}

	err := config.Validate(c)
	if !isValidationError(err) {
		t.Fatalf("err = %v, want ValidationError", err)
	}

	if !strings.Contains(err.Error(), "invalid config.build-mode") {
		t.Errorf("err message: %v", err)
	}
}

func TestValidate_CargoRequiresBuildMode(t *testing.T) {
	t.Parallel()

	c := &config.Config{Artifacts: []config.Artifact{{Name: "rust-app", ProjectType: projecttype.Cargo}}}

	err := config.Validate(c)
	if !isValidationError(err) {
		t.Fatalf("err = %v, want ValidationError", err)
	}

	if !strings.Contains(err.Error(), "requires config.build-mode") {
		t.Errorf("err message: %v", err)
	}
}

func TestValidate_CargoRejectsInvalidBuildMode(t *testing.T) {
	t.Parallel()

	c := &config.Config{Artifacts: []config.Artifact{{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		Name:        "rust-app",
		ProjectType: projecttype.Cargo,
		Cargo:       &config.CargoConfig{BuildMode: "native"},
	}}}

	err := config.Validate(c)
	if !isValidationError(err) {
		t.Fatalf("err = %v, want ValidationError", err)
	}

	if !strings.Contains(err.Error(), "invalid config.build-mode") {
		t.Errorf("err message: %v", err)
	}
}

// TestValidate_RejectsMultipleArtifactFirstCargoDepsForOneContainer
// mirrors the equivalent Go test. publish-container.yml exposes one
// `cargo-artifact-name` slot per container; two artefact-first cargo
// deps would collide on it, so the validator rejects this shape at
// config-parse time.
func TestValidate_RejectsMultipleArtifactFirstCargoDepsForOneContainer(t *testing.T) {
	t.Parallel()

	c := &config.Config{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		Artifacts: []config.Artifact{
			{Name: "cli-a", ProjectType: projecttype.Cargo, Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeArtifactFirst}},
			{Name: "cli-b", ProjectType: projecttype.Cargo, Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeArtifactFirst}},
			{Name: "svc", ProjectType: projecttype.Cargo, Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeContainerFirst}},
		},
		Containers: []config.Container{{
			Name: "combined",
			From: []string{"cli-a", "cli-b", "svc"},
		}},
	}

	err := config.Validate(c)
	if !isValidationError(err) {
		t.Fatalf("err = %v, want ValidationError", err)
	}

	for _, want := range []string{"combined", "multiple artifact-first Cargo artifacts", "cli-a", "cli-b"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err should mention %q: %v", want, err)
		}
	}
}

// TestValidate_BuildSecrets covers the contract on the new
// containers[].build-secrets field. Names must be env-var-shaped, must
// not collide with reusable-ci-reserved secret names, and must not
// repeat. The empty/unset case is fine (feature is opt-in).
func TestValidate_BuildSecrets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		secrets    []string
		wantError  bool
		wantSubstr string
	}{
		{name: "empty list", secrets: nil, wantError: false},
		{name: "single valid", secrets: []string{"DB_PASSWORD"}, wantError: false},
		{name: "multiple valid", secrets: []string{"DB_PASSWORD", "API_TOKEN", "FOO_BAR_123"}, wantError: false},
		{name: "lowercase rejected", secrets: []string{"db_password"}, wantError: true, wantSubstr: "not a valid env-var name"},
		{name: "starts with digit", secrets: []string{"1FOO"}, wantError: true, wantSubstr: "not a valid env-var name"},
		{name: "hyphenated", secrets: []string{"DB-PASSWORD"}, wantError: true, wantSubstr: "not a valid env-var name"},
		{name: "reserved RELEASE_GPG_PRIVATE_KEY", secrets: []string{"RELEASE_GPG_PRIVATE_KEY"}, wantError: true, wantSubstr: "reusable-ci-reserved secret name"},
		{name: "reserved NPM_TOKEN", secrets: []string{"NPM_TOKEN"}, wantError: true, wantSubstr: "reusable-ci-reserved secret name"},
		{name: "reserved envelope name itself", secrets: []string{"REUSABLE_CI_BUILD_SECRETS_JSON"}, wantError: true, wantSubstr: "reusable-ci-reserved secret name"},
		{name: "duplicate", secrets: []string{"DB_PASSWORD", "DB_PASSWORD"}, wantError: true, wantSubstr: "twice"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := &config.Config{
				Artifacts: []config.Artifact{{Name: "app", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeArtifactFirst}}},
				Containers: []config.Container{{
					Name:         "img",
					From:         []string{"app"},
					BuildSecrets: tc.secrets,
				}},
			}

			err := config.Validate(c)
			if tc.wantError {
				if !isValidationError(err) {
					t.Fatalf("err = %v, want ValidationError", err)
				}

				if tc.wantSubstr != "" && !strings.Contains(err.Error(), tc.wantSubstr) {
					t.Errorf("err should mention %q: %v", tc.wantSubstr, err)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidate_RejectsMultipleArtifactFirstGoDepsForOneContainer(t *testing.T) {
	t.Parallel()

	c := &config.Config{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		Artifacts: []config.Artifact{
			{Name: "api", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeArtifactFirst}},    //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			{Name: "worker", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeArtifactFirst}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			{Name: "service", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}},
		},
		Containers: []config.Container{{
			Name: "combined",
			From: []string{"api", "worker", "service"},
		}},
	}

	err := config.Validate(c)
	if !isValidationError(err) {
		t.Fatalf("err = %v, want ValidationError", err)
	}

	for _, want := range []string{"combined", "multiple artifact-first Go artifacts", "api", "worker"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err should mention %q: %v", want, err)
		}
	}
}

func TestValidate_RejectsDuplicateNames(t *testing.T) {
	t.Parallel()

	c := &config.Config{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		Artifacts: []config.Artifact{
			{Name: "x", ProjectType: projecttype.Maven},
			{Name: "x", ProjectType: projecttype.NPM},
		},
	}

	err := config.Validate(c)
	if !isValidationError(err) {
		t.Fatalf("err = %v, want ValidationError", err)
	}

	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("err message: %v", err)
	}
}

func TestValidate_RejectsEmptyArtifactName(t *testing.T) {
	t.Parallel()

	c := &config.Config{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		Artifacts: []config.Artifact{
			{Name: "", ProjectType: projecttype.Maven},
		},
	}

	err := config.Validate(c)
	if !isValidationError(err) {
		t.Errorf("err = %v, want ValidationError", err)
	}
}

// TestValidate_RejectsEnvLeakingWorkingDirectory codifies the
// 12-Factor App split inside artifacts.yml: working-directory describes
// what to build (application configuration, ships with the artefact)
// and must not encode environment-specific state. Absolute paths,
// `..`-escaping, and `${…}` shell refs are all rejected at parse time.
func TestValidate_RejectsEnvLeakingWorkingDirectory(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		dir  string
		want string
	}{
		{name: "absolute path", dir: "/srv/build/svc", want: "must be relative"},
		{name: "env-var ref", dir: "${WORKSPACE}/svc", want: "shell-style reference"},
		{name: "parent escape", dir: "../outside", want: "escapes the workspace"},
		{name: "parent dot escape", dir: "..", want: "escapes the workspace"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := &config.Config{Artifacts: []config.Artifact{{
				Name: "svc", ProjectType: projecttype.Maven, WorkingDirectory: tc.dir,
			}}}

			err := config.Validate(c)
			if !isValidationError(err) {
				t.Fatalf("err = %v, want ValidationError for %q", err, tc.dir)
			}

			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err for %q should mention %q: %v", tc.dir, tc.want, err)
			}
		})
	}
}

// TestValidate_RejectsPathTraversalInArtefactNames pins input hygiene
// for scalar fields that flow into constructed filenames. Not a
// security boundary (the runner is fresh per job) but a foot-gun
// guard: a `binary-name: "../tmp/x"` lands the compiled file one dir
// up from where upload-artifact / release-attach look for it, surfacing
// as "file not found" deep in the publish stage rather than a clear
// rejection at config-parse.
func TestValidate_RejectsPathTraversalInArtefactNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{
			name: "artifact name with slash",
			cfg:  &config.Config{Artifacts: []config.Artifact{{Name: "svc/bad", ProjectType: projecttype.Maven}}},
			want: "path separator",
		},
		{
			name: "go binary-name with parent escape",
			cfg: &config.Config{Artifacts: []config.Artifact{{
				Name:        "svc",
				ProjectType: projecttype.Go,
				Go:          &config.GoConfig{BuildMode: config.GoBuildModeArtifactFirst, BinaryName: "../bad"},
			}}},
			want: "path separator",
		},
		{
			name: "cargo binary-name dot-segment",
			cfg: &config.Config{Artifacts: []config.Artifact{{
				Name:        "svc",
				ProjectType: projecttype.Cargo,
				Cargo:       &config.CargoConfig{BuildMode: config.CargoBuildModeArtifactFirst, BinaryName: ".."},
			}}},
			want: "dot-segment",
		},
		{
			name: "artifact name with backslash",
			cfg:  &config.Config{Artifacts: []config.Artifact{{Name: `svc\bad`, ProjectType: projecttype.Maven}}},
			want: "path separator",
		},
		{
			name: "artifact name with newline control char",
			cfg:  &config.Config{Artifacts: []config.Artifact{{Name: "svc\nrogue", ProjectType: projecttype.Maven}}},
			want: "control character",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := config.Validate(tc.cfg)
			if !isValidationError(err) {
				t.Fatalf("err = %v, want ValidationError", err)
			}

			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err should mention %q: %v", tc.want, err)
			}
		})
	}
}

// TestValidate_AcceptsNormalArtefactNames pins the positive path: the
// shapes adopters use in practice stay valid. Includes dashed names
// (`my-app`), dotted versions (`my.app`), and the typical empty
// binary-name (Go/Cargo derive from module/package name).
func TestValidate_AcceptsNormalArtefactNames(t *testing.T) {
	t.Parallel()

	cases := []*config.Config{
		{Artifacts: []config.Artifact{{Name: "my-app", ProjectType: projecttype.Maven}}},
		{Artifacts: []config.Artifact{{Name: "my.app", ProjectType: projecttype.NPM}}},
		{Artifacts: []config.Artifact{{
			Name: "svc", ProjectType: projecttype.Go,
			Go: &config.GoConfig{BuildMode: config.GoBuildModeArtifactFirst, BinaryName: "my-cli"},
		}}},
		{Artifacts: []config.Artifact{{
			Name: "svc", ProjectType: projecttype.Cargo,
			Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeArtifactFirst},
		}}},
	}
	for _, cfg := range cases {
		if err := config.Validate(cfg); err != nil {
			t.Errorf("%+v rejected: %v", cfg.Artifacts[0], err)
		}
	}
}

// TestValidate_AcceptsRelativeWorkingDirectory pins the positive path:
// reasonable relative paths stay valid. Empty (= repo root) is allowed.
func TestValidate_AcceptsRelativeWorkingDirectory(t *testing.T) {
	t.Parallel()

	for _, dir := range []string{"", ".", "services/api", "./apps/web", "deeply/nested/path"} {
		c := &config.Config{Artifacts: []config.Artifact{{
			Name: "svc", ProjectType: projecttype.Maven, WorkingDirectory: dir,
		}}}
		if err := config.Validate(c); err != nil {
			t.Errorf("dir %q rejected: %v", dir, err)
		}
	}
}

func TestValidate_ContainerReferencesUnknownArtifact(t *testing.T) {
	t.Parallel()

	c := &config.Config{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		Artifacts: []config.Artifact{
			{Name: "real", ProjectType: projecttype.Maven},
		},
		Containers: []config.Container{
			{Name: "ct", From: []string{"real", "missing"}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
	}

	err := config.Validate(c)
	if !isValidationError(err) {
		t.Fatalf("err = %v, want ValidationError", err)
	}

	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("err should mention missing artefact: %v", err)
	}
}

func TestValidate_ReportsAllViolations(t *testing.T) {
	t.Parallel()

	c := &config.Config{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		Artifacts: []config.Artifact{
			{Name: "x", ProjectType: "rust"},
			{Name: "x", ProjectType: projecttype.Maven}, // dup
		},
		Containers: []config.Container{
			{Name: "ct", From: []string{"missing"}},
		},
	}

	err := config.Validate(c)
	if err == nil {
		t.Fatal("expected error")
	}

	ve := func() *config.ValidationError {
		target := &config.ValidationError{}
		_ = errors.As(err, &target)

		return target
	}()
	if len(ve.Violations) < 3 {
		t.Errorf("expected ≥3 violations, got %d: %v", len(ve.Violations), ve.Violations)
	}
}

func TestWarnings_MavenAppToGitHubPackages(t *testing.T) {
	t.Parallel()

	c := &config.Config{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		Artifacts: []config.Artifact{
			{
				Name: "app", ProjectType: projecttype.Maven,
				BuildType: "application",
				PublishTo: []config.PublishTarget{config.PublishGitHubPackages},
			},
		},
	}

	w := config.Warnings(c)
	if len(w) != 1 || !strings.Contains(w[0], "Maven application") {
		t.Errorf("Warnings = %v, want 1 maven-app warning", w)
	}
}

func TestWarnings_LibraryToGitHubPackagesIsOK(t *testing.T) {
	t.Parallel()

	c := &config.Config{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		Artifacts: []config.Artifact{
			{
				Name: "lib", ProjectType: projecttype.Maven, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				BuildType: "library",
				PublishTo: []config.PublishTarget{config.PublishGitHubPackages},
			},
		},
	}
	if w := config.Warnings(c); len(w) != 0 {
		t.Errorf("Warnings = %v, want none for library", w)
	}
}

func isValidationError(err error) bool {
	var ve *config.ValidationError

	return errors.As(err, &ve)
}
