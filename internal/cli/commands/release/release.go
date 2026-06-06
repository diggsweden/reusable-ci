// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package release wires `reusable-ci release <subcmd>`.
//
// The subcommand tree groups related operations:
//
//   - gpg.go         — `gpg <import|cleanup>` lifecycle of the release GPG key
//   - resolve.go     — `resolve <artifact-name|metadata>` name/metadata helpers
//   - attachments.go — `attachments <plan|upload>` release-attachment lifecycle
//   - artifacts.go   — `sign`, `download-artifacts`
//   - checksums.go   — `checksums` (write the SHA256 manifest)
//   - create.go      — `create` (create the platform release)
//   - notes.go       — `notes`, `verify-changelog`
//   - sbomzip.go     — `sbom-zip` (bundle layered SBOMs)
package release

import "github.com/urfave/cli/v3"

// New returns the `release` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "release",
		Usage: "release-flow helpers (GPG lifecycle, signing, checksums, notes, create, attachments, …)",
		Commands: []*cli.Command{
			gpgGroup(),
			resolveGroup(),
			attachmentsGroup(),
			signCmd(),
			downloadArtifactsCmd(),
			checksumsCmd(),
			sbomZipCmd(),
			notesCmd(),
			verifyChangelogCmd(),
			createCmd(),
		},
	}
}
