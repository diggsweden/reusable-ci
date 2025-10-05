// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

// errMultipleYAMLDocuments is returned when artifacts.yml holds a second
// document. A second decode that succeeds gives no error of its own, so the
// condition needs a value of its own to report.
var errMultipleYAMLDocuments = errors.New("multiple YAML documents are not allowed")

// invalidConfig annotates err with context and classifies it as
// errs.ErrInvalidConfig — but only when a deeper layer has not already done
// so. Every layer here wraps the one below, and re-attaching the sentinel at
// each step produced the doubled tail an operator actually reads:
//
//	artifact "web" (npm): config has unknown or wrong-typed key: …:
//	invalid configuration: invalid configuration
//
// Classify once, at the layer that knows the meaning; contextualise above it.
func invalidConfig(err error, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if errors.Is(err, errs.ErrInvalidConfig) {
		return fmt.Errorf("%s: %w", msg, err)
	}

	return fmt.Errorf("%s: %w: %w", msg, err, errs.ErrInvalidConfig)
}

// Parse turns a byte slice (artifacts.yml content) into a typed Config.
// It does not validate or compute derived fields — call Validate next,
// then Derive when you need the SBOM defaults / container enrichment.
//
// Decoding is strict at EVERY level: a typo'd top-level key
// (`artifactz:`), a typo'd artifact key (`project-typ:`), or an unknown
// per-ecosystem `config:` key all fail here with the offending key named,
// rather than being silently dropped and surfacing as a pipeline that
// does nothing. The consumer's config file is the one input a human
// hand-types, so it gets the same strictness the codebase applies to
// itself. See (*Artifact).UnmarshalYAML for the artifact/ecosystem levels.
func Parse(data []byte) (*Config, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("config: empty input: %w", errs.ErrInvalidConfig)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		// A file holding only comments or blank lines has no document at
		// all; the decoder reports that as io.EOF, which is not a key error.
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("config: no YAML document in artifacts.yml (empty or comments only): %w", errs.ErrInvalidConfig)
		}

		return nil, invalidConfig(err, "config: unknown or wrong-typed key in artifacts.yml")
	}

	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errMultipleYAMLDocuments
		}

		return nil, invalidConfig(err, "config: trailing YAML document in artifacts.yml")
	}

	return &cfg, nil
}

// UnmarshalYAML decodes one entry under `artifacts:`. The common
// top-level fields are decoded normally; the freeform `config:` block
// is captured as a raw yaml.Node and then dispatched into the typed
// per-ecosystem sub-struct selected by ProjectType with strict mode
// (KnownFields(true)).
//
// We use a private alias type for the surface fields to break the
// recursion that would otherwise occur (yaml.Node.Decode into Artifact
// would call this method again). The Config and BuildType fields are yaml.Node (not
// *yaml.Node) because yaml.v3 only populates the AST for value-typed
// node fields — pointer fields come back zero-valued.
func (a *Artifact) UnmarshalYAML(value *yaml.Node) error {
	type artifactSurface struct {
		Name                 string           `yaml:"name"`
		ProjectType          projecttype.Type `yaml:"project-type"`
		WorkingDirectory     string           `yaml:"working-directory,omitempty"`
		BuildType            yaml.Node        `yaml:"build-type,omitempty"`
		PublishTo            []PublishTarget  `yaml:"publish-to,omitempty"`
		SBOMs                string           `yaml:"sboms,omitempty"`
		RequireAuthorization bool             `yaml:"require-authorization,omitempty"`
		Config               yaml.Node        `yaml:"config,omitempty"`
	}

	var s artifactSurface //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err := strictDecodeNode(value, &s); err != nil {
		return invalidConfig(err, "artifact entry has an unknown or wrong-typed key")
	}

	var buildType BuildType
	if s.BuildType.Kind != 0 {
		if err := s.BuildType.Decode(&buildType); err != nil {
			return invalidConfig(err, "artifact %q (%s): invalid build-type", s.Name, s.ProjectType)
		}
		// Only omission may keep the zero value: it has distinct publish filtering.
		if buildType == "" {
			return fmt.Errorf("artifact %q (%s): build-type must not be empty or null; omit build-type to use the default: %w", s.Name, s.ProjectType, errs.ErrInvalidConfig)
		}
	}

	a.Name = s.Name
	a.ProjectType = s.ProjectType
	a.WorkingDirectory = s.WorkingDirectory
	a.BuildType = buildType
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
//
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
	body, err := yaml.Marshal(selfContained(n))
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

// selfContained copies a fragment, defining external anchors at their first
// use and renaming anchors uniquely. Repeated and recursive references stay
// aliases so yaml.v3 can enforce its cycle and expansion limits during decode;
// eagerly expanding them here would bypass those checks.
func selfContained(node *yaml.Node) *yaml.Node {
	copied := make(map[*yaml.Node]*yaml.Node)

	var clone func(*yaml.Node) *yaml.Node

	clone = func(node *yaml.Node) *yaml.Node {
		if node == nil {
			return nil
		}

		if node.Kind == yaml.AliasNode && node.Alias != nil {
			node = node.Alias
		}

		if target, ok := copied[node]; ok {
			return &yaml.Node{Kind: yaml.AliasNode, Value: target.Anchor, Alias: target}
		}

		out := *node

		copied[node] = &out
		if out.Anchor != "" {
			out.Anchor = fmt.Sprintf("config%d", len(copied))
		}

		if len(node.Content) > 0 {
			out.Content = make([]*yaml.Node, len(node.Content))
			for i, child := range node.Content {
				out.Content[i] = clone(child)
			}
		}

		return &out
	}

	return clone(node)
}

// ecosystemError wraps a strict-decode failure with the artifact name
// and ecosystem so the operator sees which entry is wrong.
func ecosystemError(a *Artifact, ecosystem string, err error) error {
	return invalidConfig(err, "artifact %q (%s): config has unknown or wrong-typed key", a.Name, ecosystem)
}
