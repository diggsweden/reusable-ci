// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	domainversion "github.com/diggsweden/reusable-ci/v3/internal/domain/version"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// AndroidReleaseBuildInput drives AndroidReleaseBuild. It embeds the shared
// ReleaseBuildOptions and adds the Android-specific knobs as configuration:
// artifact-name composition, keystore signing, secrets.properties, task
// resolution, and the pinned SBOM plugin.
type AndroidReleaseBuildInput struct {
	ReleaseBuildOptions

	// Artifact-name composition (ArtifactName from the embed is the override).
	IncludeDateStamp   bool
	ArtifactNamePrefix string
	RepoName           string
	ProductFlavor      string

	// Signing credentials are passed only to this run's Gradle children.
	// Password bytes are preserved; whitespace-only aliases are refused.
	EnableSigning           bool
	KeystoreBase64          string
	KeystorePassword        string
	KeyAlias                string
	KeyPassword             string
	SecretsPropertiesBase64 string

	// TempDir is the run context's scratch directory, used to place the
	// decoded keystore outside the project dir. The CLI binding reads
	// $CI_TEMP_DIR / $RUNNER_TEMP via flag sources. Empty uses the canonical
	// OS temp root. The parent must exist. Every run creates and cleans a
	// private child below it.
	TempDir string

	// Build-task selection (GradleTasksOverride wins; else resolved).
	GradleTasksOverride string
	BuildTypes          string
	IncludeAAB          bool
	BuildModule         string

	// SBOMToolVersion pins the cyclonedx-gradle-plugin.
	SBOMToolVersion string
}

// AndroidReleaseBuild runs the whole Android release build as one step: compose
// artifact names, make the wrapper executable, decode the signing keystore (when
// enabled), resolve metadata + build tasks, write secrets.properties, run the
// gradle build, generate the Build SBOM, and append the SBOM status.
//
// It is the Android sibling of GradleReleaseBuild — the binary-owned build
// sequence (Design Rule 1). Signing files live through build and SBOM and are
// cleaned on return. Injected properties are create-only; caller files are not
// replaced. The keystore path and credentials use the child environment; the
// resolved artifact names + version are emitted on the sink as the job outputs
// downstream upload/publish jobs consume.
//
//nolint:varnamelen,cyclop,gocognit,gocyclo // ordered preflight, optional staging and all-exit cleanup stay together in this linear orchestration.
func AndroidReleaseBuild(ctx context.Context, sink ci.OutputSink, summarySink ci.SummarySink, ops AndroidGradleOps, annot output.Annotator, w, stderr io.Writer, in AndroidReleaseBuildInput) (err error) {
	dir, err := filepath.Abs(defaultDir(in.Dir))
	if err != nil {
		return err
	}

	in.Dir = dir

	// Validate both blobs before publishing outputs or staging either secret.
	// The standalone writers also validate their own inputs.
	var keystore, properties []byte

	if in.EnableSigning {
		if in.KeystoreBase64 == "" {
			return fmt.Errorf("ANDROID_KEYSTORE secret not found but enable-signing is true: %w", errs.ErrPermissionDenied)
		}

		keystore, err = decodeMobileSecret(in.KeystoreBase64)
		if err != nil {
			return fmt.Errorf("decode keystore base64: %w", err)
		}
	}

	if in.SecretsPropertiesBase64 != "" {
		properties, err = decodeMobileSecret(in.SecretsPropertiesBase64)
		if err != nil {
			return fmt.Errorf("decode secrets.properties base64: %w", err)
		}
	}

	tasks := strings.TrimSpace(in.GradleTasksOverride)
	if tasks == "" {
		tasks, err = build.ResolveAndroidBuildTasks(build.ResolveAndroidBuildTasksInput{
			Flavor:      in.ProductFlavor,
			BuildTypes:  in.BuildTypes,
			IncludeAAB:  in.IncludeAAB,
			BuildModule: in.BuildModule,
		})
		if err != nil {
			return fmt.Errorf("resolve Android build tasks: %w", err)
		}
	}
	// Tabs and spaces separate task names, so the string as a whole is not a
	// scalar; each task that becomes an argv entry is.
	for _, task := range strings.Fields(tasks) {
		if err = validateScalarValue(task); err != nil {
			// The task string is caller-controlled; name the field, not the value.
			return fmt.Errorf("resolved Android build tasks: %w: %w", err, errs.ErrValidation)
		}
	}
	// These compose the artifact names this build publishes as job outputs, so
	// they have to be printable text before a name is derived from them, not
	// after a sink refuses one.
	for _, field := range []struct{ name, value string }{
		{"artifact-name", in.ArtifactName}, {"artifact-name-prefix", in.ArtifactNamePrefix},
		{"repository-name", in.RepoName}, {"product-flavor", in.ProductFlavor},
	} {
		if err = validateScalarValue(field.value); err != nil {
			return fmt.Errorf("%s: %w", field.name, err)
		}
	}

	if in.EnableBuildSBOM && !domainversion.IsStableSemverTag("v"+in.SBOMToolVersion) {
		return fmt.Errorf("SBOM tool version must be an exact MAJOR.MINOR.PATCH: %w", errs.ErrUsage)
	}

	if err = validateAndroidCredentials(in); err != nil {
		return err
	}

	project, err := pathsafe.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open Android project: %w", err)
	}
	defer func() { err = errors.Join(err, project.Close()) }()

	wrapper, err := openAndroidBuildFile(project, "gradlew")
	if err != nil {
		return err
	}

	defer func() { err = errors.Join(err, wrapper.Close()) }()

	if err = checkAndroidMetadata(project); err != nil {
		return err
	}

	if in.SecretsPropertiesBase64 != "" {
		if _, statErr := project.Lstat("secrets.properties"); !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("injected secrets.properties requires an absent destination: %w", errors.Join(errs.ErrValidation, statErr))
		}

		if err = checkBuildWritableRoot(project); err != nil {
			return err
		}
	}

	if in.EnableSigning {
		in.TempDir, err = androidKeystoreTempDir(in.TempDir, dir)
		if err != nil {
			return err
		}
	}

	if in.EnableBuildSBOM {
		if _, err = gradleSBOMTempDir(); err != nil {
			return fmt.Errorf("preflight Gradle SBOM scratch: %w", err)
		}
	}

	if err = AndroidArtifactNames(ctx, sink, stderr, AndroidArtifactNamesInput{
		IncludeDate: in.IncludeDateStamp,
		Prefix:      in.ArtifactNamePrefix,
		RepoName:    in.RepoName,
		Flavor:      in.ProductFlavor,
		Override:    in.ArtifactName,
	}); err != nil {
		return fmt.Errorf("compose artifact names: %w", err)
	}

	if err = wrapper.Chmod(0o755); err != nil { //nolint:gosec // project wrapper must be executable; descriptor was checked before outputs.
		return fmt.Errorf("make gradlew executable: %w", err)
	}

	bound := androidProjectGradle{ops: ops, dir: dir}

	if in.EnableSigning {
		stage, stageErr := pathsafe.NewArtifactStaging(in.TempDir)
		if stageErr != nil {
			return fmt.Errorf("stage Android keystore: %w", stageErr)
		}
		defer func() {
			if cleanupErr := stage.Close(); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("clean Android keystore: %w", cleanupErr))
			}
		}()

		if err = stage.Root().WriteFile("release.keystore", keystore, 0o600); err != nil {
			return fmt.Errorf("write Android keystore: %w", err)
		}

		bound.env = []string{
			"ANDROID_KEYSTORE_PATH=" + filepath.Join(stage.Root().Name(), "release.keystore"),
			"ANDROID_KEYSTORE_PASSWORD=" + in.KeystorePassword,
			"ANDROID_KEY_ALIAS=" + in.KeyAlias,
			"ANDROID_KEY_PASSWORD=" + in.KeyPassword,
		}
	}

	if err = AndroidVersionInfo(ctx, sink, stderr, annot, AndroidVersionInfoInput{Dir: in.Dir}); err != nil {
		return fmt.Errorf("resolve version: %w", err)
	}

	if in.SecretsPropertiesBase64 != "" {
		cleanup, createErr := createAndroidProperties(project, properties)
		if createErr != nil {
			return fmt.Errorf("write secrets.properties: %w", createErr)
		}
		defer func() { err = errors.Join(err, cleanup()) }()
	}

	if err = AndroidGradleBuild(ctx, bound, w, stderr, AndroidGradleBuildInput{Tasks: tasks, SkipTests: in.SkipTests}); err != nil {
		return fmt.Errorf("gradle build: %w", err)
	}

	return androidSBOMStep(ctx, gradleDeps{summary: summarySink, ops: bound, w: w, stderr: stderr}, in)
}

// AndroidGradleOps adds per-invocation environment overrides without widening
// GradleOps for generic Gradle consumers. The adapter retains inherited runtime
// variables (including JDK/SDK settings); this is not an environment sandbox.
type AndroidGradleOps interface {
	RunInDirEnvInherit(ctx context.Context, dir string, env []string, stdout, stderr io.Writer, args ...string) error
}

type androidProjectGradle struct {
	ops AndroidGradleOps
	dir string
	env []string
}

func (g androidProjectGradle) RunInherit(ctx context.Context, stdout, stderr io.Writer, args ...string) error {
	return g.ops.RunInDirEnvInherit(ctx, g.dir, g.env, stdout, stderr, args...)
}

func (g androidProjectGradle) RunInDirInherit(ctx context.Context, _ string, stdout, stderr io.Writer, args ...string) error {
	return g.RunInherit(ctx, stdout, stderr, args...)
}

func androidSBOMStep(ctx context.Context, deps gradleDeps, in AndroidReleaseBuildInput) error {
	outcome := outcomeSkipped

	var generationErr error

	if in.EnableBuildSBOM {
		outcome = outcomeSuccess
		if err := GradleSBOM(ctx, deps.ops, deps.w, deps.stderr, GradleSBOMInput{CycloneDXVersion: in.SBOMToolVersion, WorkingDir: in.Dir}); err != nil {
			outcome = outcomeFailure
			generationErr = fmt.Errorf("gradle-android Build SBOM generation failed: %w", err)
		}
	}

	if err := appsummary.BuildSBOMStatus(ctx, deps.summary, appsummary.BuildSBOMStatusInput{
		Ecosystem: "gradle-android",
		Outcome:   outcome,
		WorkDir:   defaultDir(in.Dir),
	}); err != nil {
		return errors.Join(generationErr, fmt.Errorf("write SBOM status: %w", err))
	}

	if generationErr != nil {
		return generationErr
	}

	return nil
}
