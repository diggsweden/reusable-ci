// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"fmt"
	"io"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
)

// CargoReleaseBuildInput drives CargoReleaseBuild. It embeds the shared
// ReleaseBuildOptions and adds the Cargo-specific compile knobs.
type CargoReleaseBuildInput struct {
	ReleaseBuildOptions

	BinaryName string
	Version    string
	RefName    string
	Platforms  string
}

// CargoBuildSBOM generates a Build-layer SBOM with cargo-cyclonedx (baked into
// the runtime image). --all covers workspaces; single-crate projects get one
// bom.json at the crate root.
func CargoBuildSBOM(ctx context.Context, tool CargoTool, w, stderr io.Writer, dir string) error {
	return tool.Run(ctx, CargoRunInput{
		Dir:    defaultCargoDir(dir),
		Args:   []string{"cyclonedx", "--all", "--format", "json", "--override-filename", "bom"},
		Stdout: w,
		Stderr: stderr,
	})
}

// CargoReleaseBuild runs the whole Cargo release build as one step: resolve
// metadata, fetch deps, test (unless skipped), generate the Build SBOM (unless
// disabled), cross-compile per platform into dist/, and append the SBOM status.
//
// It is the Cargo sibling of GoReleaseBuild — the binary-owned build sequence
// (Design Rule 1). Metadata is threaded in-process (no $CI_OUTPUT round-trip);
// the SBOM is best-effort (a failure warns + reports failure status but does not
// fail the build), mirroring the workflow's `if: always()` status step.
//
//nolint:varnamelen // idiomatic short names (w/in) — testing/http/io conventions, matching the sibling build funcs.
func CargoReleaseBuild(ctx context.Context, summarySink ci.SummarySink, tool CargoTool, w, stderr io.Writer, in CargoReleaseBuildInput) error {
	meta, err := resolveCargoMetadata(ctx, tool, CargoMetadataInput{
		Dir:          in.Dir,
		ArtifactName: in.ArtifactName,
		BinaryName:   in.BinaryName,
		Version:      in.Version,
		RefName:      in.RefName,
	})
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(w, "Cargo release build: binary=%s package=%s version=%s\n", meta.binaryName, meta.packageName, meta.version)

	if err := CargoFetch(ctx, tool, w, stderr, in.Dir); err != nil {
		return fmt.Errorf("cargo fetch: %w", err)
	}

	if !in.SkipTests {
		if err := CargoTest(ctx, tool, w, stderr, CargoTestInput{Dir: in.Dir}); err != nil {
			return fmt.Errorf("cargo test: %w", err)
		}
	}

	outcome := outcomeSkipped

	if in.EnableBuildSBOM {
		outcome = outcomeSuccess
		if err := CargoBuildSBOM(ctx, tool, w, stderr, in.Dir); err != nil {
			outcome = outcomeFailure
			_, _ = fmt.Fprintf(stderr, "WARN: cargo Build SBOM generation failed (continuing): %v\n", err)
		}
	}

	if err := CargoBuildBinaries(ctx, tool, w, stderr, CargoBuildBinariesInput{
		Dir:        in.Dir,
		BinaryName: meta.binaryName,
		Platforms:  in.Platforms,
		Version:    meta.version,
	}); err != nil {
		return fmt.Errorf("build binaries: %w", err)
	}

	if err := appsummary.SBOMCountStatus(ctx, summarySink, appsummary.SBOMCountStatusInput{
		Kind:    "cargo",
		Outcome: outcome,
		WorkDir: defaultCargoDir(in.Dir),
	}); err != nil {
		return fmt.Errorf("write SBOM status: %w", err)
	}

	return nil
}
