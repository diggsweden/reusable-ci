// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom

import (
	"context"
	"io"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cargo"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gotool"
	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

// buildSBOMGenerator implements appsbom.BuildSBOMGenerator by delegating to the
// build use cases for the lockfile-reading ecosystems (go via cyclonedx-gomod,
// cargo via cargo-cyclonedx) — the same generators `build <eco> run` uses. This
// is the composition seam: it lets `sbom assemble` generate the build layer
// while app/sbom stays free of app/build + adapter imports. maven/gradle/npm
// emit the BOM only as a build byproduct, so they return ErrUnsupported and are
// harvested instead.
type buildSBOMGenerator struct{}

func (buildSBOMGenerator) GenerateBuildSBOM(ctx context.Context, projectType projecttype.Type, dir, name string, stderr io.Writer) error {
	switch projectType {
	case projecttype.Go:
		return appbuild.GoBuildSBOM(ctx, gotool.CycloneDXGoMod{}, stderr, stderr, appbuild.GoBuildSBOMInput{Dir: dir, ArtifactName: name})
	case projecttype.Cargo:
		return appbuild.CargoBuildSBOM(ctx, cargo.New(), stderr, stderr, dir)
	default:
		return errs.ErrUnsupported
	}
}
