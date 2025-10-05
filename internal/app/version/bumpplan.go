// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	domainversion "github.com/diggsweden/reusable-ci/v3/internal/domain/version"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// BumpPlanInput drives BumpPlan.
type BumpPlanInput struct {
	PlanJSON      string
	Version       string
	ChangelogFile string
	MavenCLIOpts  []string
	XcconfigFile  string
}

// defaultChangelogFile is the path the changelog verbs assume when the caller
// names none. Shared so the three defaulting sites cannot drift apart.
const defaultChangelogFile = "CHANGELOG.md"

// BumpPlan applies every planned version mutation in one working tree. This is
// deliberately sequential: one release produces one commit from one authorized
// source SHA rather than a matrix of jobs racing to advance the same branch.
//
//nolint:cyclop,gocognit // separate whole-list preflight from mutation and ordered pathspec union; no transaction framework.
func BumpPlan(ctx context.Context, ops BumpOps, sink ci.OutputSink, out, stderr io.Writer, annot output.Annotator, in BumpPlanInput) error {
	var plan pipeline.ReleasePrepareStagePlan
	if err := json.Unmarshal([]byte(in.PlanJSON), &plan); err != nil {
		return fmt.Errorf("parse prepare-stage plan: %w: %w", err, errs.ErrMalformedInput)
	}

	if plan.Version != pipeline.ReleasePlanVersion || plan.Stage != "prepare" {
		return fmt.Errorf("unsupported prepare-stage plan version/stage %d/%q: %w", plan.Version, plan.Stage, errs.ErrInvalidConfig)
	}

	if strings.TrimSpace(in.Version) == "" {
		return fmt.Errorf("version is required: %w", errs.ErrUsage)
	}

	if !plan.Targets.VersionBump.Runs {
		return setBumpPlanPattern(ctx, sink, "")
	}

	changelog := in.ChangelogFile
	if changelog == "" {
		changelog = defaultChangelogFile
	}

	changelogPath, err := filepath.Abs(changelog)
	if err != nil {
		return err
	}
	// Use a literal absolute filename, not the CLI stdin sentinel.
	body, err := cliio.ReadFile(changelogPath)
	if err != nil {
		return fmt.Errorf("read full changelog %q: %w", changelog, err)
	}

	if len(body) == 0 {
		return fmt.Errorf("full changelog %q is empty: %w", changelog, errs.ErrMissingInput)
	}
	// Validate the engine-owned local prerequisites for every item before changing
	// the first project. This is not rollback of later tool or filesystem failures.
	inputs := make([]bumpTarget, 0, len(plan.Targets.VersionBump.Items))
	for _, artifact := range plan.Targets.VersionBump.Items {
		dir, err := normalizedPlanWorkingDir(artifact.WorkingDirectory)
		if err != nil {
			return err
		}

		gradleVersionFile := ""
		if artifact.Gradle != nil {
			gradleVersionFile = artifact.Gradle.GradleVersionFile
		}

		if artifact.GradleAndroid != nil {
			gradleVersionFile = artifact.GradleAndroid.GradleVersionFile
		}

		bump, err := normalizeBumpTarget(BumpInput{ProjectType: artifact.ProjectType, Version: in.Version, WorkingDir: dir,
			GradleVersionFile: gradleVersionFile, XcconfigFile: in.XcconfigFile, MavenCLIOpts: in.MavenCLIOpts})
		if err != nil {
			return fmt.Errorf("preflight artifact %q: %w", artifact.Name, err)
		}

		if !supportedPlanLiteralPath(bump.file) {
			return fmt.Errorf("preflight artifact %q: version file cannot be represented by file-pattern: %w", artifact.Name, errs.ErrUsage)
		}

		if err := preflightPlanBump(ops, bump); err != nil {
			return fmt.Errorf("preflight artifact %q: %w", artifact.Name, err)
		}

		inputs = append(inputs, bump)
	}

	if err := validatePlanBumpAliases(inputs, changelogPath); err != nil {
		return err
	}

	pathspecs := make([]string, 0, len(plan.Targets.VersionBump.Items)*3)
	seen := make(map[string]struct{})

	for index, artifact := range plan.Targets.VersionBump.Items {
		bump := inputs[index]

		workingDir := bump.WorkingDir
		if err := installPlanChangelog(workingDir, body); err != nil {
			return fmt.Errorf("artifact %q: %w", artifact.Name, err)
		}

		if err := Bump(ctx, ops, out, stderr, annot, bump.BumpInput); err != nil {
			return fmt.Errorf("bump artifact %q: %w", artifact.Name, err)
		}

		patterns := strings.Fields(domainversion.FilePattern(artifact.ProjectType))
		if bump.file != "" {
			patterns = append(patterns, bump.file)
		}

		for _, pathspec := range patterns {
			pathspec = prefixPlanPathspec(workingDir, pathspec)
			if _, ok := seen[pathspec]; ok {
				continue
			}

			seen[pathspec] = struct{}{}
			pathspecs = append(pathspecs, pathspec)
		}
	}

	for _, pathspec := range strings.Fields(plan.FilePattern) {
		if _, exists := seen[pathspec]; !exists {
			seen[pathspec] = struct{}{}
			pathspecs = append(pathspecs, pathspec)
		}
	}

	return setBumpPlanPattern(ctx, sink, strings.Join(pathspecs, " "))
}

// Compare the changelog source and engine-owned destinations, not native outputs.
// Prerequisites have been read-checked; identity checks assume a single writer
// and stable paths. Follow input symlinks just as cliio.ReadFile does.
//
//nolint:cyclop,gocognit // one bounded inventory and pairwise contract check, without a general writer framework.
func validatePlanBumpAliases(inputs []bumpTarget, changelogPath string) error {
	type fileRole struct {
		path   string
		info   os.FileInfo
		writer projecttype.Type // Empty for changelog source/copies, which may share a file.
		source bool
	}

	sourceInfo, err := os.Stat(changelogPath)
	if err != nil {
		return planBumpReadError(err)
	}

	files := make([]fileRole, 0, len(inputs)*2+1)
	files = append(files, fileRole{path: changelogPath, info: sourceInfo, source: true})

	for _, in := range inputs {
		for index, file := range []string{defaultChangelogFile, in.file} {
			if file == "" {
				continue
			}

			path, err := filepath.Abs(filepath.Join(in.WorkingDir, file))
			if err != nil {
				return err
			}

			info, err := os.Stat(path)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return planBumpReadError(err)
			}

			target := fileRole{path: path, info: info}
			if index == 1 {
				target.writer = in.ProjectType
			}

			files = append(files, target)
		}
	}

	for index, left := range files {
		for _, right := range files[index+1:] {
			if left.writer == "" && right.writer == "" {
				continue
			}

			same := left.path == right.path || (left.info != nil && right.info != nil && os.SameFile(left.info, right.info))
			if !same {
				continue
			}

			if left.source || right.source {
				return fmt.Errorf("preflight primary version target/changelog source alias: %q and %q: %w", left.path, right.path, errs.ErrValidation)
			}

			if left.writer == "" || right.writer == "" {
				return fmt.Errorf("preflight primary version target/changelog destination alias: %q and %q: %w", left.path, right.path, errs.ErrValidation)
			}
			// Cargo requires TOML strings; property writers emit unquoted values.
			// Gradle, Android and Xcode otherwise write distinct property keys.
			if (left.writer == projecttype.Cargo) != (right.writer == projecttype.Cargo) {
				return fmt.Errorf("preflight incompatible primary version writers %s and %s share %q and %q: %w", left.writer, right.writer, left.path, right.path, errs.ErrValidation)
			}
		}
	}

	return nil
}

func preflightPlanBump(ops BumpOps, in bumpTarget) error {
	if _, err := readPlanBumpFile(in.WorkingDir, defaultChangelogFile, true); err != nil {
		return err
	}

	switch in.ProjectType {
	case projecttype.Maven:
		if ops.Maven == nil {
			return fmt.Errorf("maven adapter not provided: %w", errs.ErrUsage)
		}
		// Maven may select another POM through CLI options or native config.
		return nil
	case projecttype.NPM:
		if ops.NPM == nil {
			return fmt.Errorf("npm adapter not provided: %w", errs.ErrUsage)
		}
		// Native npm prefix/workspace selection remains the tool's contract.
		return nil
	default:
	}

	if in.file == "" {
		return nil
	}

	body, err := readPlanBumpFile(in.WorkingDir, in.file, in.ProjectType == projecttype.XcodeIOS)
	if err != nil {
		return err
	}

	if in.ProjectType == projecttype.Cargo {
		_, _, err = domainversion.UpdateCargoVersion(string(body), in.Version)
	}

	return err
}

// Missing output files are allowed, but parents must already be real directories.
// Existing inputs/outputs must be readable, regular and nonlinked. These checks
// make no writes and cannot freeze external tools or concurrent filesystem changes.
func readPlanBumpFile(dir, relative string, optional bool) ([]byte, error) {
	if !pathsafe.Relative(relative) || filepath.Clean(relative) == "." {
		return nil, fmt.Errorf("version/changelog file must be relative to the project: %w", errs.ErrUsage)
	}

	root, err := pathsafe.OpenRoot(filepath.Join(dir, filepath.Dir(relative)))
	if err != nil {
		return nil, planBumpReadError(err)
	}

	defer func() { _ = root.Close() }()

	name := filepath.Base(relative)

	info, err := root.Lstat(name)
	if optional && errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read version/changelog file %q: %w", relative, planBumpReadError(err))
	}

	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("version/changelog file %q must be nonlinked and regular: %w", relative, errs.ErrValidation)
	}

	return cliio.ReadFileInRoot(root, name)
}

func planBumpReadError(err error) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("%w: %w", err, errs.ErrMissingInput)
	case errors.Is(err, os.ErrPermission):
		return fmt.Errorf("%w: %w", err, errs.ErrPermissionDenied)
	default:
		return err
	}
}

func normalizedPlanWorkingDir(dir string) (string, error) {
	// Reject raw whitespace rather than selecting a different directory by trimming.
	if !supportedPlanLiteralPath(dir) {
		return "", fmt.Errorf("working directory cannot be represented by file-pattern: %w", errs.ErrInvalidConfig)
	}

	dir = filepath.Clean(dir) // Exactly empty retains the current-directory default.
	if !pathsafe.Relative(dir) {
		return "", fmt.Errorf("working directory must stay inside the checkout: %q: %w", dir, errs.ErrInvalidConfig)
	}

	return dir, nil
}

// Only engine-owned literal paths use this guard. Default and custom pathspecs
// retain their intentional Git syntax; standalone Bump has no pattern transport.
func supportedPlanLiteralPath(path string) bool {
	return !strings.ContainsFunc(path, unicode.IsSpace) &&
		!strings.ContainsAny(path, "\x00*?[\\") &&
		!strings.HasPrefix(filepath.Clean(path), ":")
}

func installPlanChangelog(workingDir string, body []byte) error {
	root, err := pathsafe.OpenRoot(workingDir)
	if err != nil {
		return fmt.Errorf("open working directory %q: %w", workingDir, err)
	}
	defer func() { _ = root.Close() }()

	if err := root.WriteFile(defaultChangelogFile, body, 0o644); err != nil {
		return fmt.Errorf("install changelog in %q: %w", workingDir, err)
	}

	return nil
}

func prefixPlanPathspec(workingDir, pathspec string) string {
	if workingDir == "." {
		return pathspec
	}

	dir := filepath.ToSlash(workingDir)
	if glob, ok := strings.CutPrefix(pathspec, ":(glob)"); ok {
		return ":(glob)" + dir + "/" + glob
	}

	return dir + "/" + filepath.ToSlash(pathspec)
}

func setBumpPlanPattern(ctx context.Context, sink ci.OutputSink, pattern string) error {
	if sink == nil {
		return nil
	}

	if err := sink.Set(ctx, "file-pattern", pattern); err != nil {
		return fmt.Errorf("emit file-pattern: %w", err)
	}

	return nil
}
