// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
)

// AndroidArtifactNamesInput drives AndroidArtifactNames. Mirrors the
// positional/env contract of scripts/android/generate-artifact-names.sh.
type AndroidArtifactNamesInput struct {
	IncludeDate bool
	Prefix      string
	RepoName    string
	Flavor      string
	Override    string

	// Today is interpolated into the date stamp. Empty → time.Now().
	Today time.Time
}

// AndroidArtifactNames computes the four artifact names and writes
// each to the OutputSink under its kebab-case key. The four
// `printf "Artifact: ..."` status lines also go to stderr to match
// the bash.
func AndroidArtifactNames(ctx context.Context, sink ci.OutputSink, stderr io.Writer, in AndroidArtifactNamesInput) error {
	today := in.Today
	if today.IsZero() {
		today = time.Now()
	}
	names, err := build.ResolveAndroidArtifactNames(build.AndroidArtifactNamesInput{
		IncludeDate: in.IncludeDate,
		Prefix:      in.Prefix,
		RepoName:    in.RepoName,
		Flavor:      in.Flavor,
		Override:    in.Override,
		Today:       today,
	})
	if err != nil {
		return err
	}

	type kv struct{ k, v string }
	for _, p := range []kv{
		{"debug-name", names.DebugName},
		{"release-name", names.ReleaseName},
		{"aab-name", names.AABName},
		{"sbom-name", names.SBOMName},
	} {
		if err := sink.Set(ctx, p.k, p.v); err != nil {
			return fmt.Errorf("set %s: %w", p.k, err)
		}
	}
	fmt.Fprintf(stderr, "Debug artifact: %s\n", names.DebugName)
	fmt.Fprintf(stderr, "Release artifact: %s\n", names.ReleaseName)
	fmt.Fprintf(stderr, "AAB artifact: %s\n", names.AABName)
	fmt.Fprintf(stderr, "SBOM artifact: %s\n", names.SBOMName)
	return nil
}

// AndroidVersionInfoInput drives AndroidVersionInfo.
type AndroidVersionInfoInput struct {
	// Dir is the directory containing gradle.properties. Empty → cwd.
	Dir string
}

// AndroidVersionInfo reads gradle.properties (if present) and writes
// `version` and `version-code` outputs. Matches the
// scripts/android/get-version-info.sh fall-through to "unknown" when
// gradle.properties is missing.
func AndroidVersionInfo(ctx context.Context, sink ci.OutputSink, stderr io.Writer, annot output.Annotator, in AndroidVersionInfoInput) error {
	dir := in.Dir
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
	}
	path := filepath.Join(dir, "gradle.properties")
	data, err := os.ReadFile(path)
	if err != nil {
		annot.Warningf("gradle.properties not found, version info unavailable")
		if err := sink.Set(ctx, "version", "unknown"); err != nil {
			return err
		}
		return sink.Set(ctx, "version-code", "unknown")
	}
	v, c := build.ParseGradleVersionFromProperties(string(data))
	if err := sink.Set(ctx, "version", v); err != nil {
		return err
	}
	if err := sink.Set(ctx, "version-code", c); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "Version: %s (%s)\n", v, c)
	return nil
}

// AndroidDecodeKeystoreInput drives AndroidDecodeKeystore.
type AndroidDecodeKeystoreInput struct {
	// Base64 is the base64-encoded keystore body (typically from the
	// $ANDROID_KEYSTORE_BASE64 secret).
	Base64 string
	// Dir is the directory the keystore is decoded into. Empty → cwd.
	Dir string
}

// AndroidDecodeKeystore base64-decodes Base64 into <Dir>/release.keystore
// (mode 0600) and prints `ANDROID_KEYSTORE_PATH=<absolute path>` to
// stdout. Mirrors scripts/android/decode-keystore.sh — the workflow
// redirects stdout to $GITHUB_ENV so the path is exposed as a process
// env var to subsequent steps (gradle reads it via System.getenv).
func AndroidDecodeKeystore(stdout, stderr io.Writer, in AndroidDecodeKeystoreInput) error {
	if in.Base64 == "" {
		return fmt.Errorf("ANDROID_KEYSTORE secret not found but enable-signing is true: %w", errs.ErrPermissionDenied)
	}
	dir := in.Dir
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
	}
	body, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in.Base64))
	if err != nil {
		return fmt.Errorf("decode keystore base64: %w: %w", err, errs.ErrMalformedInput)
	}
	path := filepath.Join(dir, "release.keystore")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("write keystore: %w", err)
	}
	fmt.Fprintf(stderr, "✓ Android keystore decoded successfully\n")
	fmt.Fprintf(stdout, "ANDROID_KEYSTORE_PATH=%s\n", path)
	return nil
}

// AndroidResolveBuildTasksInput drives AndroidResolveBuildTasks.
type AndroidResolveBuildTasksInput struct {
	Flavor      string
	BuildTypes  string
	IncludeAAB  bool
	BuildModule string
}

// AndroidResolveBuildTasks computes the gradle task list and writes
// it to the OutputSink under `tasks`. Mirrors
// scripts/android/resolve-build-tasks.sh.
func AndroidResolveBuildTasks(ctx context.Context, sink ci.OutputSink, stderr io.Writer, in AndroidResolveBuildTasksInput) error {
	tasks := build.ResolveAndroidBuildTasks(build.ResolveAndroidBuildTasksInput{
		Flavor:      in.Flavor,
		BuildTypes:  in.BuildTypes,
		IncludeAAB:  in.IncludeAAB,
		BuildModule: in.BuildModule,
	})
	if err := sink.Set(ctx, "tasks", tasks); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "Building with tasks: %s\n", tasks)
	return nil
}

// AndroidGradleBuildInput drives AndroidGradleBuild.
type AndroidGradleBuildInput struct {
	// Tasks is the space-separated gradle task list (typically the
	// output of AndroidResolveBuildTasks).
	Tasks string
	// SkipTests adds `-x test` to the gradle invocation.
	SkipTests bool
}

// AndroidGradleBuild runs `./gradlew <tasks> [-x test]`. Mirrors
// scripts/android/build-gradle.sh.
func AndroidGradleBuild(ctx context.Context, ops GradleOps, stdout, stderr io.Writer, in AndroidGradleBuildInput) error {
	if strings.TrimSpace(in.Tasks) == "" {
		return fmt.Errorf("GRADLE_TASKS is required: %w", errs.ErrUsage)
	}
	fmt.Fprintf(stdout, "Running Gradle tasks: %s\n", in.Tasks)
	args := strings.Fields(in.Tasks)
	if in.SkipTests {
		args = append(args, "-x", "test")
	}
	return ops.RunInherit(ctx, stdout, stderr, args...)
}

// AndroidListArtifactsInput drives AndroidListArtifacts.
type AndroidListArtifactsInput struct {
	// BuildModule is typically "app". The directory walked is
	// <BuildModule>/build/outputs.
	BuildModule string
	// Root is the project root. Empty → cwd.
	Root string
}

// AndroidListArtifacts walks <BuildModule>/build/outputs for *.apk and
// *.aab files and prints their paths to stdout. Mirrors
// scripts/android/list-built-artifacts.sh — the bash uses `find … -ls`
// for ls-style output; this Go version just prints the absolute path
// per line, which is what downstream readers consume.
func AndroidListArtifacts(stdout io.Writer, in AndroidListArtifactsInput) error {
	if in.BuildModule == "" {
		return fmt.Errorf("BUILD_MODULE is required: %w", errs.ErrUsage)
	}
	root := in.Root
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
	}
	outputs := filepath.Join(root, in.BuildModule, "build", "outputs")
	fmt.Fprintf(stdout, "Built artifacts:\n")
	any := false
	walkErr := filepath.WalkDir(outputs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Treat "outputs/" missing as "no artifacts" rather than a hard error.
			if os.IsNotExist(err) {
				return filepath.SkipAll
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext == ".apk" || ext == ".aab" {
			fmt.Fprintln(stdout, path)
			any = true
		}
		return nil
	})
	if walkErr != nil {
		return walkErr
	}
	if !any {
		fmt.Fprintln(stdout, "No artifacts found")
	}
	return nil
}
