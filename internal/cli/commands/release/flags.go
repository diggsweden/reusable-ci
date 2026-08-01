// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

// Shared flag names, declared once so the repeated literals across the
// release subcommands (assemble, sign, checksum, sbom-zip, create, resolve,
// provenance) stay under goconst's budget and rename in one place.
const (
	flagOutput              = "output"
	flagVersion             = "version"
	flagAssembly            = "assembly"
	flagReleaseArtifactsDir = "release-artifacts-dir"
	flagSBOMDir             = "sbom-dir"
	flagAttachArtifacts     = "attach-artifacts"
	flagRepository          = "repository"
	flagArtifactName        = "artifact-name"
	flagTag                 = "tag"
	flagPrivateKeyFile      = "private-key-file"
)
