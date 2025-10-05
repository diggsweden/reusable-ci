// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

// ReleaseBuildOptions are the knobs every ecosystem's consolidated
// `build <eco> run` shares. Each ecosystem's run-input embeds this and adds its
// own toolchain-specific fields, so the common build contract is defined once
// and adding an ecosystem reuses it rather than re-spreading the same fields.
type ReleaseBuildOptions struct {
	// Dir is the project root (containing go.mod / package.json / …).
	Dir string
	// ArtifactName overrides the upload/SBOM naming; empty uses the
	// ecosystem default (module/package basename).
	ArtifactName string
	// SkipTests skips the in-build test run.
	SkipTests bool
	// EnableBuildSBOM toggles Build-layer SBOM generation.
	EnableBuildSBOM bool
}

// Build-SBOM step outcomes reported to the stage summary by every ecosystem's
// `build <eco> run`. Defined once here (the shared build contract) rather than
// in any single ecosystem's run file.
const (
	outcomeSuccess = "success"
	outcomeFailure = "failure"
	outcomeSkipped = "skipped"
)
