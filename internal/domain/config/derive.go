// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package config

import (
	"fmt"
	"slices"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

// Derive applies the post-parse computed fields used by downstream
// config consumers: default sbom values, effective sbom layers, and the
// container fields derived from referenced artifacts.
func Derive(cfg *Config) error {
	if cfg == nil {
		return nil
	}

	for i := range cfg.Artifacts {
		a := &cfg.Artifacts[i]
		sbomsValue := a.SBOMs
		if sbomsValue == "" {
			if SBOMSupportedTypes[a.ProjectType] {
				sbomsValue = "all"
			} else {
				sbomsValue = "none"
			}
			a.SBOMs = sbomsValue
		}
		eff, err := ExpandSBOMs(sbomsValue)
		if err != nil {
			return fmt.Errorf("artefact %q: %w", a.Name, err)
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

// ArtifactsByProjectType returns the subset of artifacts matching pt.
func ArtifactsByProjectType(artifacts []Artifact, pt projecttype.Type) []Artifact {
	out := make([]Artifact, 0, len(artifacts))
	for _, a := range artifacts {
		if a.ProjectType == pt {
			out = append(out, a)
		}
	}
	return out
}

// ArtifactsByPublishTarget returns the subset of artifacts that publish
// to target. Maven applications are excluded from github-packages to
// match the bash emission rules.
func ArtifactsByPublishTarget(artifacts []Artifact, target PublishTarget) []Artifact {
	out := make([]Artifact, 0, len(artifacts))
	for _, a := range artifacts {
		if !slices.Contains(a.PublishTo, target) {
			continue
		}
		if target == PublishGitHubPackages &&
			a.ProjectType == projecttype.Maven &&
			a.BuildType == BuildTypeApplication {
			continue
		}
		out = append(out, a)
	}
	return out
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
		c := &cfg.Containers[i]
		typesSeen := map[projecttype.Type]bool{}
		analyzedContainer := false
		for _, dep := range c.From {
			a, ok := byName[dep]
			if !ok {
				continue
			}
			typesSeen[a.ProjectType] = true
			for _, l := range a.EffectiveSBOMs {
				if l == SBOMLayerAnalyzedContainer {
					analyzedContainer = true
				}
			}
		}
		types := make([]projecttype.Type, 0, len(typesSeen))
		for _, pt := range ValidProjectTypes {
			if typesSeen[pt] {
				types = append(types, pt)
			}
		}
		c.ArtifactTypes = types
		c.EnableAnalyzedContainerSBOM = analyzedContainer

		if len(c.BuildArgs) > 0 {
			keys := make([]string, 0, len(c.BuildArgs))
			for k := range c.BuildArgs {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			lines := make([]string, len(keys))
			for j, k := range keys {
				lines[j] = k + "=" + c.BuildArgs[k]
			}
			c.BuildArgsString = strings.Join(lines, "\n")
		}
	}
}
