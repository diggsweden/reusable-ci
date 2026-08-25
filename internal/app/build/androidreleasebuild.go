// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
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

	// Signing. The keystore base64 is decoded to a 0600 file outside the
	// project dir; the per-key passwords stay env vars the gradle build reads.
	EnableSigning           bool
	KeystoreBase64          string
	SecretsPropertiesBase64 string

	// TempDir is the run context's scratch directory, used to place the
	// decoded keystore outside the project dir. The CLI binding reads
	// $CI_TEMP_DIR / $RUNNER_TEMP via flag sources. Empty → os.MkdirTemp.
	TempDir string

	// Build-task selection (GradleTasksOverride wins; else resolved).
	GradleTasksOverride string
	BuildTypes          string
	IncludeAAB          bool
	BuildModule         string

	// Library selects Android *library* mode: build-type: library on a
	// gradle-android artifact. It is the whole difference — there is no
	// separate project type and no setup-android flag. A library builds
	// release-only and AAB-free, and is never keystore-signed: its Maven
	// Central signature is the GPG release key applied in
	// publish-gradle.yml, so the keystore decode below is skipped too.
	Library bool

	// SBOMToolVersion pins the cyclonedx-gradle-plugin.
	SBOMToolVersion string
}

// AndroidReleaseBuild runs the whole Android release build as one step: compose
// artifact names, make the wrapper executable, decode the signing keystore (when
// enabled), resolve metadata + build tasks, write secrets.properties, run the
// gradle build, generate the Build SBOM, and append the SBOM status.
//
// It is the Android sibling of GradleReleaseBuild — the binary-owned build
// sequence (Design Rule 1). The keystore path is threaded to the gradle build
// in-process via the environment (matching how the build script reads it); the
// resolved artifact names + version are emitted on the sink as the job outputs
// downstream upload/publish jobs consume.
//
//nolint:varnamelen,cyclop // idiomatic short names (w/in); linear build sequence with one branch per optional step (signing/secrets/sbom).
func AndroidReleaseBuild(ctx context.Context, sink ci.OutputSink, summarySink ci.SummarySink, ops GradleOps, annot output.Annotator, w, stderr io.Writer, in AndroidReleaseBuildInput) error {
	if err := AndroidArtifactNames(ctx, sink, stderr, AndroidArtifactNamesInput{
		IncludeDate: in.IncludeDateStamp,
		Prefix:      in.ArtifactNamePrefix,
		RepoName:    in.RepoName,
		Flavor:      in.ProductFlavor,
		Override:    in.ArtifactName,
	}); err != nil {
		return fmt.Errorf("compose artifact names: %w", err)
	}

	if err := makeGradlewExecutable(in.Dir); err != nil {
		return err
	}

	// A library is never keystore-signed (see the Library field), so the
	// decode is skipped even when the caller left --enable-signing on: an
	// application-shaped default must not put a keystore on disk for a
	// build that has no use for one.
	if in.EnableSigning && !in.Library {
		// Decode outside the working dir (Dir empty → RUNNER_TEMP/mktemp) so no
		// artifact upload can pick the keystore up. gradle reads the path from
		// the environment, alongside the per-key password secrets.
		path, err := decodeAndroidKeystore(AndroidDecodeKeystoreInput{Base64: in.KeystoreBase64, TempDir: in.TempDir})
		if err != nil {
			return err
		}

		if err := os.Setenv("ANDROID_KEYSTORE_PATH", path); err != nil {
			return fmt.Errorf("set ANDROID_KEYSTORE_PATH: %w", err)
		}
	}

	if err := AndroidVersionInfo(ctx, sink, stderr, annot, AndroidVersionInfoInput{Dir: in.Dir}); err != nil {
		return fmt.Errorf("resolve version: %w", err)
	}

	tasks := strings.TrimSpace(in.GradleTasksOverride)
	if tasks == "" {
		tasks = build.ResolveAndroidBuildTasks(build.ResolveAndroidBuildTasksInput{
			Flavor:      in.ProductFlavor,
			BuildTypes:  in.BuildTypes,
			IncludeAAB:  in.IncludeAAB,
			BuildModule: in.BuildModule,
			Library:     in.Library,
		})
	}

	if in.SecretsPropertiesBase64 != "" {
		if err := AndroidWriteSecretsProperties(w, AndroidWriteSecretsPropertiesInput{Base64: in.SecretsPropertiesBase64, Dir: in.Dir}); err != nil {
			return fmt.Errorf("write secrets.properties: %w", err)
		}
	}

	if err := AndroidGradleBuild(ctx, ops, w, stderr, AndroidGradleBuildInput{Tasks: tasks, SkipTests: in.SkipTests}); err != nil {
		return fmt.Errorf("gradle build: %w", err)
	}

	return androidSBOMStep(ctx, gradleDeps{summary: summarySink, ops: ops, w: w, stderr: stderr}, in)
}

func androidSBOMStep(ctx context.Context, deps gradleDeps, in AndroidReleaseBuildInput) error {
	outcome := outcomeSkipped

	if in.EnableBuildSBOM {
		outcome = outcomeSuccess
		if err := GradleSBOM(ctx, deps.ops, deps.w, deps.stderr, GradleSBOMInput{CycloneDXVersion: in.SBOMToolVersion, WorkingDir: in.Dir}); err != nil {
			outcome = outcomeFailure

			_, _ = fmt.Fprintf(deps.stderr, "WARN: gradle-android Build SBOM generation failed (continuing): %v\n", err)
		}
	}

	if err := appsummary.BuildSBOMStatus(ctx, deps.summary, appsummary.BuildSBOMStatusInput{
		Ecosystem: "gradle-android",
		Outcome:   outcome,
		WorkDir:   defaultDir(in.Dir),
	}); err != nil {
		return fmt.Errorf("write SBOM status: %w", err)
	}

	return nil
}
