// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config

import (
	"fmt"
	"slices"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

// Derive applies the post-parse computed fields used by downstream
// config consumers: default sbom values, effective sbom layers, and the
// container fields derived from referenced artifacts.
func Derive(cfg *Config) error {
	if cfg == nil {
		return nil
	}

	for i := range cfg.Artifacts {
		a := &cfg.Artifacts[i] //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

		sbomsValue := a.SBOMs
		if sbomsValue == "" {
			if SBOMSupportedTypes[a.ProjectType] {
				sbomsValue = "all" //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			} else {
				sbomsValue = "none" //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			}

			a.SBOMs = sbomsValue
		}

		eff, err := ExpandSBOMs(sbomsValue)
		if err != nil {
			return fmt.Errorf("artifact %q: %w", a.Name, err)
		}

		a.EffectiveSBOMs = eff
	}

	resolveContainers(cfg)

	return nil
}

// AnyRequireAuthorization reports whether any artifact enables the
// release authorization gate.
func AnyRequireAuthorization(artifacts []Artifact) bool {
	for _, a := range artifacts {
		if a.RequireAuthorization {
			return true
		}
	}

	return false
}

// GoArtifactBuildMode returns the build mode declared on a Go artifact.
// The typed Artifact.Go sub-struct (populated by Parse with strict YAML
// decoding) is the canonical source. Returns "" for non-Go artifacts or
// when build-mode was omitted.
func GoArtifactBuildMode(a Artifact) GoBuildMode {
	if a.ProjectType != projecttype.Go || a.Go == nil {
		return ""
	}

	return GoBuildMode(strings.TrimSpace(string(a.Go.BuildMode)))
}

// CargoArtifactBuildMode mirrors GoArtifactBuildMode for Cargo artifacts.
// Returns "" for non-Cargo artifacts or when build-mode was omitted.
func CargoArtifactBuildMode(a Artifact) CargoBuildMode {
	if a.ProjectType != projecttype.Cargo || a.Cargo == nil {
		return ""
	}

	return CargoBuildMode(strings.TrimSpace(string(a.Cargo.BuildMode)))
}

// SupportedPublishTarget reports whether the current workflows support
// publishing artifact a to target. It intentionally models the public v3
// contract, not every schema-recognised future value.
func SupportedPublishTarget(a Artifact, target PublishTarget) bool { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	switch target {
	case PublishForgePackages:
		return a.ProjectType == projecttype.Maven || a.ProjectType == projecttype.NPM
	case PublishMavenCentral:
		return a.ProjectType == projecttype.Maven && a.BuildType == BuildTypeLibrary
	case PublishGooglePlay:
		return a.ProjectType == projecttype.GradleAndroid
	case PublishNPMJS:
		return false
	default:
		return false
	}
}

func resolveContainers(cfg *Config) {
	if len(cfg.Containers) == 0 {
		return
	}

	byName := make(map[string]*Artifact, len(cfg.Artifacts))
	for i := range cfg.Artifacts {
		byName[cfg.Artifacts[i].Name] = &cfg.Artifacts[i]
	}

	for i := range cfg.Containers {
		resolveContainer(&cfg.Containers[i], byName)
	}
}

// resolveContainer fills the derived fields on one Container based on
// the artifacts it depends on (via From) and its own BuildArgs.
func resolveContainer(c *Container, byName map[string]*Artifact) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	scan := scanContainerDeps(c.From, byName)

	types := make([]projecttype.Type, 0, len(scan.typesSeen))
	for _, pt := range ValidProjectTypes {
		if scan.typesSeen[pt] {
			types = append(types, pt)
		}
	}

	c.ArtifactTypes = types
	c.EnableAnalyzedContainerSBOM = scan.analyzedContainer
	c.GoArtifactName = scan.goArtifactName
	c.CargoArtifactName = scan.cargoArtifactName

	c.BuildArgsString = formatBuildArgs(c.BuildArgs)
}

// containerDepScan summarises everything resolveContainer needs from
// one walk of a container's From list. Returning a struct (rather than
// four positional results) keeps the call site readable as the set of
// artifact-first ecosystems grows.
type containerDepScan struct {
	typesSeen         map[projecttype.Type]bool
	analyzedContainer bool
	goArtifactName    string
	cargoArtifactName string
}

// scanContainerDeps walks one container's From list and returns the set
// of project types seen, whether any depended-on artifact emits an
// analyzed-container SBOM, and the first Go / Cargo artifact-first
// dep's name (if any). One slot per ecosystem because publish-container.yml
// carries one *-artifact-name input per language.
func scanContainerDeps(from []string, byName map[string]*Artifact) containerDepScan {
	scan := containerDepScan{typesSeen: map[projecttype.Type]bool{}}

	for _, dep := range from {
		a, ok := byName[dep] //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if !ok {
			continue
		}

		scan.typesSeen[a.ProjectType] = true
		if a.ProjectType == projecttype.Go && GoArtifactBuildMode(*a) == GoBuildModeArtifactFirst && scan.goArtifactName == "" {
			scan.goArtifactName = a.Name
		}

		if a.ProjectType == projecttype.Cargo && CargoArtifactBuildMode(*a) == CargoBuildModeArtifactFirst && scan.cargoArtifactName == "" {
			scan.cargoArtifactName = a.Name
		}

		if slices.Contains(a.EffectiveSBOMs, SBOMLayerAnalyzedContainer) {
			scan.analyzedContainer = true
		}
	}

	return scan
}

// formatBuildArgs renders a Container.BuildArgs map as a deterministic,
// newline-joined "key=value" string. Empty input returns "".
func formatBuildArgs(args map[string]string) string {
	if len(args) == 0 {
		return ""
	}

	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = k + "=" + args[k]
	}

	return strings.Join(lines, "\n")
}
