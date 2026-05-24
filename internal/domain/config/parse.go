// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package config

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

// Parse turns a byte slice (artifacts.yml content) into a typed Config.
// It does not validate or compute derived fields — call Validate next,
// then Derive when you need the SBOM defaults / container enrichment.
//
// The `config:` block on each artifact is decoded directly into the
// typed per-ecosystem sub-struct (Artifact.Maven, .GradleAndroid,
// .XcodeIOS, .Go, etc.) selected by ProjectType, with strict mode so
// unknown or wrong-typed keys fail here rather than silently downstream.
// See (*Artifact).UnmarshalYAML.
func Parse(data []byte) (*Config, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("config: empty input: %w", errs.ErrInvalidConfig)
	}

	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("config: parse yaml: %w: %w", err, errs.ErrInvalidConfig)
	}

	return &c, nil
}

// UnmarshalYAML decodes one entry under `artifacts:`. The common
// top-level fields are decoded normally; the freeform `config:` block
// is captured as a raw yaml.Node and then dispatched into the typed
// per-ecosystem sub-struct selected by ProjectType with strict mode
// (KnownFields(true)).
//
// We use a private alias type for the surface fields to break the
// recursion that would otherwise occur (yaml.Node.Decode into Artifact
// would call this method again). The Config field is yaml.Node (not
// *yaml.Node) because yaml.v3 only populates the AST for value-typed
// node fields — pointer fields come back zero-valued.
func (a *Artifact) UnmarshalYAML(value *yaml.Node) error {
	type artifactSurface struct {
		Name                 string           `yaml:"name"`
		ProjectType          projecttype.Type `yaml:"project-type"`
		WorkingDirectory     string           `yaml:"working-directory,omitempty"`
		BuildType            BuildType        `yaml:"build-type,omitempty"`
		PublishTo            []PublishTarget  `yaml:"publish-to,omitempty"`
		SBOMs                string           `yaml:"sboms,omitempty"`
		RequireAuthorization bool             `yaml:"require-authorization,omitempty"`
		Config               yaml.Node        `yaml:"config,omitempty"`
	}

	var s artifactSurface //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err := value.Decode(&s); err != nil {
		return err
	}

	a.Name = s.Name
	a.ProjectType = s.ProjectType
	a.WorkingDirectory = s.WorkingDirectory
	a.BuildType = s.BuildType
	a.PublishTo = s.PublishTo
	a.SBOMs = s.SBOMs
	a.RequireAuthorization = s.RequireAuthorization

	// Kind == 0 marks the zero-value Node (no `config:` key present).
	if s.Config.Kind == 0 {
		return nil
	}

	return decodeEcosystemConfig(a, &s.Config)
}

// decodeEcosystemConfig strict-decodes the raw config:-block node into
// the typed sub-struct on a matching ProjectType. Unknown keys fail.
//nolint:cyclop // decode dispatch: one branch per supported ecosystem (npm/maven/gradle/...).
func decodeEcosystemConfig(a *Artifact, node *yaml.Node) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	switch a.ProjectType {
	case projecttype.Maven:
		var c MavenConfig
		if err := strictDecodeNode(node, &c); err != nil {
			return ecosystemError(a, "maven", err)
		}

		a.Maven = &c
	case projecttype.NPM:
		var c NPMConfig
		if err := strictDecodeNode(node, &c); err != nil {
			return ecosystemError(a, "npm", err)
		}

		a.NPM = &c
	case projecttype.Gradle:
		var c GradleConfig
		if err := strictDecodeNode(node, &c); err != nil {
			return ecosystemError(a, "gradle", err)
		}

		a.Gradle = &c
	case projecttype.GradleAndroid:
		var c GradleAndroidConfig
		if err := strictDecodeNode(node, &c); err != nil {
			return ecosystemError(a, "gradle-android", err)
		}

		a.GradleAndroid = &c
	case projecttype.XcodeIOS:
		var c XcodeIOSConfig
		if err := strictDecodeNode(node, &c); err != nil {
			return ecosystemError(a, "xcode-ios", err)
		}

		a.XcodeIOS = &c
	case projecttype.Go:
		var c GoConfig
		if err := strictDecodeNode(node, &c); err != nil {
			return ecosystemError(a, "go", err)
		}

		a.Go = &c
	case projecttype.Cargo:
		var c CargoConfig
		if err := strictDecodeNode(node, &c); err != nil {
			return ecosystemError(a, "cargo", err)
		}

		a.Cargo = &c
	case projecttype.Python:
		var c PythonConfig
		if err := strictDecodeNode(node, &c); err != nil {
			return ecosystemError(a, "python", err)
		}

		a.Python = &c
	case projecttype.Meta, projecttype.Auto, projecttype.Unknown:
		// No typed config sub-struct for these — the YAML loader
		// should not see a `config:` block here. Reject it loudly so
		// operators don't silently lose data.
		return fmt.Errorf(
			"artifact %q (project-type %q) does not accept a config block: %w",
			a.Name, a.ProjectType, errs.ErrInvalidConfig,
		)
	}

	return nil
}

// strictDecodeNode round-trips a yaml.Node through Marshal+NewDecoder so
// KnownFields(true) can reject unknown keys — yaml.Node.Decode itself
// has no strict-mode flag. The round-trip is cheap (config blocks are
// O(10) fields each) and avoids a parallel-walker code path that would
// drift from the canonical decoder.
func strictDecodeNode(n *yaml.Node, out any) error {
	body, err := yaml.Marshal(n)
	if err != nil {
		return fmt.Errorf("re-marshal config: %w", err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(body))
	dec.KnownFields(true)

	if err := dec.Decode(out); err != nil {
		return err
	}

	return nil
}

// ecosystemError wraps a strict-decode failure with the artifact name
// and ecosystem so the operator sees which entry is wrong.
func ecosystemError(a *Artifact, ecosystem string, err error) error {
	return fmt.Errorf(
		"artifact %q (%s): config has unknown or wrong-typed key: %w: %w",
		a.Name, ecosystem, err, errs.ErrInvalidConfig,
	)
}
