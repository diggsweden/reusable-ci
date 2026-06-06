// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

// Shared subcommand names + flag names + flag usage strings used by
// the per-toolchain build subgroups. Centralised here so adding a new
// toolchain doesn't drift away from the established surface.

const (
	// Subcommand names common to multiple toolchains.
	subCmdCompile  = "compile"
	subCmdMetadata = "metadata"

	// Flag names shared across cargo / go / gradle-android CLI surfaces.
	flagBinaryName   = "binary-name"
	flagArtifactName = "artifact-name"
	flagVersion      = "version"
	flagWorkingDir   = "working-dir"
	flagRefName      = "ref-name"

	// Flag-usage strings duplicated between cargo and go CLI definitions.
	usageVersionRef     = "git ref name used to derive the version when --version is empty"
	usageCargoDirectory = "directory containing the Cargo.toml file"
)
