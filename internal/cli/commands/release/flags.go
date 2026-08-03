// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

// Shared flag names, declared once so the repeated literals across the
// release subcommands (assemble, sign, checksum, sbom-zip, create, resolve,
// provenance) stay under goconst's budget and rename in one place.
const (
	flagOutput                   = "output"
	flagVersion                  = "version"
	flagAssembly                 = "assembly"
	flagReleaseArtifactsDir      = "release-artifacts-dir"
	flagSBOMDir                  = "sbom-dir"
	flagAttachArtifacts          = "attach-artifacts"
	flagRepository               = "repository"
	flagArtifactName             = "artifact-name"
	flagTag                      = "tag"
	flagKey                      = "key"
	flagOIDCIssuer               = "oidc-issuer"
	flagPrivateKeyFile           = "private-key-file"
	flagPassphraseFile           = "passphrase-file"
	flagManifest                 = "manifest"
	flagChecksumFile             = "checksum-file"
	flagChecksumsFile            = "checksums-file"
	flagDistDir                  = "dist-dir"
	flagReleaseSHA               = "release-sha"
	flagArtifactTransferPlanJSON = "artifact-transfer-plan-json"
)

// Shared flag defaults and credential labels, declared once for the same
// goconst/rename-in-one-place reasons as the flag names above.
const (
	// defaultDistDir is the default dist directory flag value.
	defaultDistDir = "dist"
	// makeLatestDefault is the default for the make-latest release flag.
	makeLatestDefault = "true"
	// defaultDistPath is the default trailing-slash dist path flag value.
	defaultDistPath = "dist/"
	// credentialGPGPrivateKey is the errs.Credential What label shared by
	// the GPG-backed signing and import commands.
	credentialGPGPrivateKey = "GPG private key" //nolint:gosec // credential label for error messages, not a secret.
)
