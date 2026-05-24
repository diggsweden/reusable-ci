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

// AndroidArtifactNamesInput drives AndroidArtifactNames.
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
	for _, p := range []kv{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		{"debug-name", names.DebugName},
		{"release-name", names.ReleaseName},
		{"aab-name", names.AABName},
		{"sbom-name", names.SBOMName},
	} {
		if err := sink.Set(ctx, p.k, p.v); err != nil {
			return fmt.Errorf("set %s: %w", p.k, err)
		}
	}

	_, _ = fmt.Fprintf(stderr, "Debug artifact: %s\n", names.DebugName)
	_, _ = fmt.Fprintf(stderr, "Release artifact: %s\n", names.ReleaseName)
	_, _ = fmt.Fprintf(stderr, "AAB artifact: %s\n", names.AABName)
	_, _ = fmt.Fprintf(stderr, "SBOM artifact: %s\n", names.SBOMName)

	return nil
}

// AndroidVersionInfoInput drives AndroidVersionInfo.
type AndroidVersionInfoInput struct {
	// Dir is the directory containing gradle.properties. Empty → cwd.
	Dir string
}

// AndroidVersionInfo reads gradle.properties (if present) and writes
// `version` and `version-code` outputs. Falls through to "unknown" for
// either field when gradle.properties is missing.
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

	data, err := os.ReadFile(path) //nolint:gosec // path is CLI-flag-derived; filename component is hardcoded.
	if err != nil {
		annot.Warningf("gradle.properties not found, version info unavailable")

		if err := sink.Set(ctx, "version", "unknown"); err != nil {
			return err
		}

		return sink.Set(ctx, "version-code", "unknown")
	}

	v, c := build.ParseGradleVersionFromProperties(string(data)) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err := sink.Set(ctx, "version", v); err != nil {
		return err
	}

	if err := sink.Set(ctx, "version-code", c); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stderr, "Version: %s (%s)\n", v, c)

	return nil
}

// AndroidDecodeKeystoreInput drives AndroidDecodeKeystore.
type AndroidDecodeKeystoreInput struct {
	// Base64 is the base64-encoded keystore body (typically from the
	// $ANDROID_KEYSTORE_BASE64 secret).
	Base64 string
	// Dir is the directory the keystore is decoded into. Empty →
	// $RUNNER_TEMP when set (the GitHub Actions runner-scoped temp
	// path, cleaned between jobs), else os.MkdirTemp. Never the
	// project working dir — that would let any later `path: .`
	// upload-artifact glob pick the keystore up by accident.
	Dir string
}

// AndroidDecodeKeystore base64-decodes Base64 into <Dir>/release.keystore
// (mode 0600) and prints `ANDROID_KEYSTORE_PATH=<absolute path>` to
// w. The workflow redirects w to $GITHUB_ENV so the path is
// exposed as a process env var to subsequent steps (gradle reads it
// via System.getenv).
//
// The output path lives OUTSIDE the project working directory by
// default — see Dir's doc-comment. This is the v4 hardening: the
// pre-v4 default of cwd meant a misconfigured `path: .` upload-artifact
// could ship the keystore; the runner-temp default closes that gap.
func AndroidDecodeKeystore(w, stderr io.Writer, in AndroidDecodeKeystoreInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.Base64 == "" {
		return fmt.Errorf("ANDROID_KEYSTORE secret not found but enable-signing is true: %w", errs.ErrPermissionDenied)
	}

	dir, err := resolveKeystoreDir(in.Dir)
	if err != nil {
		return err
	}

	body, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in.Base64))
	if err != nil {
		return fmt.Errorf("decode keystore base64: %w: %w", err, errs.ErrMalformedInput)
	}

	path := filepath.Join(dir, "release.keystore")
	if writeErr := os.WriteFile(path, body, 0o600); writeErr != nil {
		return fmt.Errorf("write keystore: %w", writeErr)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve keystore path: %w", err)
	}

	_, _ = fmt.Fprintf(stderr, "✓ Android keystore decoded successfully\n")
	_, _ = fmt.Fprintf(w, "ANDROID_KEYSTORE_PATH=%s\n", absPath)

	return nil
}

// resolveKeystoreDir applies the runner-temp-first default. Explicit
// Dir wins (callers know what they're doing); $RUNNER_TEMP wins next
// (the standard GHA scratch path, cleaned between jobs); a fresh
// os.MkdirTemp is the last fallback so the keystore never lands in
// cwd, no matter what the runner is.
func resolveKeystoreDir(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

	if runnerTemp := os.Getenv("RUNNER_TEMP"); runnerTemp != "" {
		return runnerTemp, nil
	}

	dir, err := os.MkdirTemp("", "reusable-ci-keystore-")
	if err != nil {
		return "", fmt.Errorf("mktemp keystore dir: %w", err)
	}

	return dir, nil
}

// AndroidWriteSecretsPropertiesInput drives AndroidWriteSecretsProperties.
//
// Mirrors AndroidDecodeKeystore's shape: secret arrives via env (Base64), the
// command writes a sensitive file at mode 0600 in the working directory.
type AndroidWriteSecretsPropertiesInput struct {
	// Base64 is the base64-encoded secrets.properties body. Empty input is a
	// no-op (the standard Android pattern: secrets.properties is optional).
	Base64 string
	// Dir is the destination directory. Defaults to the current working directory.
	Dir string
}

// AndroidWriteSecretsProperties base64-decodes Base64 into
// <Dir>/secrets.properties (mode 0600). Gradle's
// secrets-gradle-plugin reads this file at configuration time.
//
// Empty Base64 is treated as "no secrets configured" and returns nil
// — the AndroidGradleBuild step works fine when secrets.properties
// is absent.
func AndroidWriteSecretsProperties(w io.Writer, in AndroidWriteSecretsPropertiesInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if strings.TrimSpace(in.Base64) == "" {
		_, _ = fmt.Fprintln(w, "No SECRETS_PROPERTIES_BASE64 configured; skipping secrets.properties write")

		return nil
	}

	dir := in.Dir
	if dir == "" {
		var err error

		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
	}
	// Allow whitespace in the base64 payload (multi-line secrets pasted via
	// GitHub's secret UI sometimes carry trailing newlines).
	body, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(in.Base64), ""))
	if err != nil {
		return fmt.Errorf("decode secrets.properties base64: %w: %w", err, errs.ErrMalformedInput)
	}

	if len(body) == 0 {
		return fmt.Errorf("SECRETS_PROPERTIES_BASE64 decoded to zero bytes: %w", errs.ErrValidation)
	}

	path := filepath.Join(dir, "secrets.properties")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("write secrets.properties: %w", err)
	}

	_, _ = fmt.Fprintln(w, "✓ secrets.properties decoded successfully")

	return nil
}

// AndroidResolveBuildTasksInput drives AndroidResolveBuildTasks.
type AndroidResolveBuildTasksInput struct {
	Override    string
	Flavor      string
	BuildTypes  string
	IncludeAAB  bool
	BuildModule string
}

// AndroidResolveBuildTasks computes the gradle task list and writes
// it to the OutputSink under `tasks`. Mirrors.
func AndroidResolveBuildTasks(ctx context.Context, sink ci.OutputSink, stderr io.Writer, in AndroidResolveBuildTasksInput) error {
	if strings.TrimSpace(in.Override) != "" {
		tasks := strings.TrimSpace(in.Override)
		if err := sink.Set(ctx, "tasks", tasks); err != nil {
			return err
		}

		_, _ = fmt.Fprintf(stderr, "Building with explicit tasks: %s\n", tasks)

		return nil
	}

	tasks := build.ResolveAndroidBuildTasks(build.ResolveAndroidBuildTasksInput{
		Flavor:      in.Flavor,
		BuildTypes:  in.BuildTypes,
		IncludeAAB:  in.IncludeAAB,
		BuildModule: in.BuildModule,
	})
	if err := sink.Set(ctx, "tasks", tasks); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stderr, "Building with tasks: %s\n", tasks)

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

// AndroidGradleBuild runs `./gradlew <tasks> [-x test]`. Mirrors.
func AndroidGradleBuild(ctx context.Context, ops GradleOps, w, stderr io.Writer, in AndroidGradleBuildInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if strings.TrimSpace(in.Tasks) == "" {
		return fmt.Errorf("gradle tasks are required: pass --tasks <list> or set $GRADLE_TASKS: %w", errs.ErrUsage)
	}

	_, _ = fmt.Fprintf(w, "Running Gradle tasks: %s\n", in.Tasks)

	args := strings.Fields(in.Tasks)
	if in.SkipTests {
		args = append(args, "-x", "test")
	}

	return ops.RunInherit(ctx, w, stderr, args...)
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
// *.aab files and prints their absolute paths to w, one per line.
//nolint:cyclop // lists APK+AAB by build-type with conditional include/skip per pattern.
func AndroidListArtifacts(w io.Writer, in AndroidListArtifactsInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.BuildModule == "" {
		return fmt.Errorf("build module is required: pass --build-module <name> or set $BUILD_MODULE: %w", errs.ErrUsage)
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

	_, _ = fmt.Fprintf(w, "Built artifacts:\n")

	found := false

	walkErr := filepath.WalkDir(outputs, func(path string, d fs.DirEntry, err error) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
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
			_, _ = fmt.Fprintln(w, path)

			found = true
		}

		return nil
	})
	if walkErr != nil {
		return walkErr
	}

	if !found {
		_, _ = fmt.Fprintln(w, "No artifacts found")
	}

	return nil
}
