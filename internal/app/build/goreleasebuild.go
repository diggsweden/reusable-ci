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

// GoReleaseBuildInput drives GoReleaseBuild — the full Go release build. It
// embeds the shared ReleaseBuildOptions (Dir, ArtifactName, SkipTests,
// EnableBuildSBOM) and adds the Go-specific compile knobs.
type GoReleaseBuildInput struct {
	ReleaseBuildOptions

	BinaryName  string
	Version     string
	RefName     string
	Commit      string
	Platforms   string
	BuildTags   string
	LDFlags     string
	MainPackage string
}

// GoReleaseBuild runs the whole Go release build as one step: resolve metadata, download
// dependencies, test (unless skipped), generate the Build SBOM (unless
// disabled), cross-compile per platform, and write the build summary.
//
// It is the binary-owned build *sequence* (Design Rule 1): the forge job wraps
// it only with platform-transport steps (checkout, cache, artifact upload), so
// the same sequence runs identically on GitHub, GitLab, and Forgejo with no
// per-forge re-encoding. The resolved metadata is threaded between steps
// in-process, so — unlike the granular subcommands — no scalar-output round
// trip (and thus no $CI_OUTPUT) is needed.
func GoReleaseBuild(ctx context.Context, summarySink ci.SummarySink, goTool GoTool, sbomTool CycloneDXGoModTool, w, stderr io.Writer, in GoReleaseBuildInput) error { //nolint:varnamelen // idiomatic short names (w/in) — testing/http/io conventions, matching the sibling build funcs.
	meta, err := resolveGoMetadata(GoMetadataInput{
		Dir:             in.Dir,
		ArtifactName:    in.ArtifactName,
		BinaryNameInput: in.BinaryName,
		VersionInput:    in.Version,
		RefName:         in.RefName,
	})
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(w, "Go release build: binary=%s module=%s version=%s\n", meta.BinaryName, meta.Module, meta.Version)

	if err := GoDownload(ctx, goTool, w, stderr, in.Dir); err != nil {
		return fmt.Errorf("download dependencies: %w", err)
	}

	if !in.SkipTests {
		if err := GoTest(ctx, goTool, w, stderr, GoTestInput{Dir: in.Dir, BuildTags: in.BuildTags}); err != nil {
			return fmt.Errorf("go test: %w", err)
		}
	}

	if in.EnableBuildSBOM {
		if err := GoBuildSBOM(ctx, sbomTool, w, stderr, GoBuildSBOMInput{
			Dir:          in.Dir,
			ArtifactName: in.ArtifactName,
			BinaryName:   meta.BinaryName,
		}); err != nil {
			return fmt.Errorf("generate build SBOM: %w", err)
		}
	}

	if err := GoBuildBinaries(ctx, goTool, w, stderr, GoBuildBinariesInput{
		Dir:         in.Dir,
		BinaryName:  meta.BinaryName,
		BuildTags:   in.BuildTags,
		LDFlags:     in.LDFlags,
		MainPackage: in.MainPackage,
		Platforms:   in.Platforms,
		Version:     meta.Version,
		Commit:      in.Commit,
	}); err != nil {
		return fmt.Errorf("build binaries: %w", err)
	}

	if err := appsummary.GoBuild(ctx, summarySink, appsummary.GoBuildInput{
		BinaryName: meta.BinaryName,
		Module:     meta.Module,
		Platforms:  in.Platforms,
		Version:    meta.Version,
		SkipTests:  in.SkipTests,
	}); err != nil {
		return fmt.Errorf("write build summary: %w", err)
	}

	return nil
}
