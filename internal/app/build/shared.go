// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

// Toolchain-build constants reused across cargo / go / npm / etc.

const (
	// archGOOSWindows is the GOOS / Cargo target_os value for Windows
	// builds. Used to gate the `.exe` suffix on emitted binary names.
	archGOOSWindows = "windows"
	// extExe is the executable suffix on Windows binaries.
	extExe = ".exe"

	// flagCargoLocked is the `--locked` cargo flag that refuses to
	// touch the lockfile during compile. Required for reproducible
	// release builds.
	flagCargoLocked = "--locked"

	// subCmdBuild is the literal subcommand name that several
	// toolchain wrappers (cargo, go, npm) pass to their underlying
	// CLI invocation.
	subCmdBuild = "build"

	// Workflow output keys emitted by toolchain build use cases.
	outKeyBinaryName = "binary-name"
	outKeyVersion    = "version"
	outKeyPackage    = "package"
)
