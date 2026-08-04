// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

// Shared subcommand names + flag names + flag usage strings used by
// the per-toolchain build subgroups. Centralised here so adding a new
// toolchain doesn't drift away from the established surface.

const (
	// Subcommand names common to multiple toolchains.
	subCmdMetadata = "metadata"
	subCmdBuild    = "build"
	subCmdRun      = "run"

	// Flag names shared across cargo / go / gradle-android CLI surfaces.
	flagBinaryName      = "binary-name"
	flagArtifactName    = "artifact-name"
	flagVersion         = "version"
	flagWorkingDir      = "working-dir"
	flagRefName         = "ref-name"
	flagPlatforms       = "platforms"
	flagBuildTags       = "build-tags"
	flagSkipTests       = "skip-tests"
	flagBuildSBOM       = "build-sbom"
	flagCLIOpts         = "cli-opts"
	flagTasks           = "tasks"
	flagSBOMToolVersion = "sbom-tool-version"
	flagRepositoryName  = "repository-name"
	flagFlavor          = "flavor"
	flagBuildModule     = "build-module"
	// flagTempDir binds the run context's scratch directory (via
	// cienv.TempDir()) so the app layer receives it instead of reading
	// $RUNNER_TEMP and missing $CI_TEMP_DIR.
	flagTempDir   = "temp-dir"
	flagWorkspace = "workspace"
	flagProject   = "project"

	// defaultBuildModule is the conventional Android gradle module.
	defaultBuildModule = "app"

	// Flag-usage strings duplicated between cargo and go CLI definitions.
	usageVersionRef     = "git ref name used to derive the version when --version is empty"
	usageCargoDirectory = "directory containing the Cargo.toml file"
	usageGoModDirectory = "directory containing the go.mod file"
	usageNPMDirectory   = "directory containing package.json"
	usageSkipTestTask   = "append -x test to skip the test task"
	usageGradleTasks    = "whitespace-separated gradle tasks to run"
)
