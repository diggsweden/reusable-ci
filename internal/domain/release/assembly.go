// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

// AssemblyVersion is the current release assembly manifest contract version.
const AssemblyVersion = 1

// DefaultAssemblyFile is the orchestrated release file-set manifest. It is
// intentionally run-local metadata, not a release asset.
const DefaultAssemblyFile = ".reusable-ci/release-assembly.json"

// DefaultReleaseFilesDir is the staged release output tree uploaded as a CI
// summary artifact and consumed by checksum/sign/create commands.
const DefaultReleaseFilesDir = "release-files"

// ReleaseAssembly records the canonical file set for a release. Orchestrated
// flows produce this once after all CI artifacts and generated SBOMs are on
// disk; checksum, signing, SBOM ZIP, and release creation then consume the same
// manifest instead of rediscovering divergent file sets.
type ReleaseAssembly struct {
	Version      int            `json:"version"`
	Assets       []AssemblyFile `json:"assets"`
	SBOMs        []AssemblyFile `json:"sboms,omitempty"`
	ChecksumFile string         `json:"checksum_file"`
	SBOMZipFile  string         `json:"sbom_zip_file,omitempty"`
}

// AssemblyFile is one staged file in a release assembly.
type AssemblyFile struct {
	// Path is the staged on-disk path used by later release commands.
	Path string `json:"path"`
	// Name is the final release asset/checksum/SBOM archive basename.
	Name string `json:"name"`

	// Source* fields preserve where the staged file came from for auditability.
	SourcePath     string `json:"source_path,omitempty"`
	SourceKind     string `json:"source_kind,omitempty"`
	SourceArtifact string `json:"source_artifact,omitempty"`
	Required       bool   `json:"required,omitempty"`
}
