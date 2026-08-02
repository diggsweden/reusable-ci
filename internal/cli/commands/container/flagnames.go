// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

// Shared flag, subcommand, and credential-string names for the container
// package. Each name that repeats across command definitions is declared
// once here, so spellings cannot drift between verbs and the package stays
// under goconst's literal budget. Values are part of the CLI contract —
// docs/cli-reference.md is generated from them and a sync test gates any
// change.
const (
	flagArch                    = "arch"
	flagArchRef                 = "arch-ref"
	flagAuthFile                = "auth-file"
	flagBaseInputID             = "base-input-id"
	flagBaseInputsJSON          = "base-inputs-json"
	flagContainerfile           = "containerfile"
	flagContentID               = "content-id"
	flagContext                 = "context"
	flagExpectedBaseRepository  = "expected-base-repository"
	flagExpectedImageRepository = "expected-image-repository"
	flagFile                    = "file"
	flagGroupsJSON              = "groups-json"
	flagImage                   = "image"
	flagLedger                  = "ledger"
	flagPlatform                = "platform"
	flagProvenanceEnvelope      = "provenance-envelope"
	flagRecursive               = "recursive"
	flagRef                     = "ref"
	flagRegistryPasswordFile    = "registry-password-file"
	flagRegistryUsername        = "registry-username"
	flagRefName                 = "ref-name"
	flagRepository              = "repository"
	flagRetryAttempts           = "retry-attempts"
	flagRetryDelaySeconds       = "retry-delay-seconds"
	flagRevision                = "revision"
	flagSBOMPathPattern         = "sbom-path-pattern"
	flagServerURL               = "server-url"
	flagSource                  = "source"
	flagSourceSHA               = "source-sha"
	flagTag                     = "tag"
	flagTLSVerify               = "tls-verify"
	flagVersion                 = "version"
)

// Subcommand names shared by the image-lifecycle trees (ledger,
// release-images, base-images, signer-image): the same verb vocabulary is
// deliberately used in every tree.
const (
	subCmdCleanup        = "cleanup"
	subCmdPromote        = "promote"
	subCmdRollback       = "rollback"
	subCmdSign           = "sign"
	subCmdValidate       = "validate"
	subCmdVerifyExisting = "verify-existing"
)

// Registry-credential strings shared by the login/push verbs. The password
// is deliberately file/env-only (never argv).
const (
	envRegistryUser           = "REGISTRY_USER"
	credRegistryUsername      = "registry username"
	credRegistryPassword      = "registry password"
	usageRegistryUsername     = "registry username; the password is read from --registry-password-file or $REGISTRY_TOKEN / $REGISTRY_PASSWORD"
	usageTLSVerify            = "verify registry TLS certificates: true or false"
	usageRegistryPasswordFile = `file containing the registry password/token ("-" reads stdin); defaults to $REGISTRY_TOKEN then $REGISTRY_PASSWORD. The password never appears in argv.`
)

// cosignMethodNoteGPG is the shared signflags method note for OCI-image
// signing verbs, where gpg cannot be a signing backend.
const cosignMethodNoteGPG = "gpg is rejected — it cannot sign OCI images."

// tlsVerifyDefault is the shared default for the --tls-verify flags of the
// push verbs: registry TLS verification stays on unless explicitly disabled.
const tlsVerifyDefault = "true"
