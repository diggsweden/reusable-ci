// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/sbom"
)

// manifestProbe pairs a filename with the project type it declares.
// Order matters only for tie-breaking in error messages — every test
// inspects ALL probes so a multi-manifest repo always reports the full
// list.
//
//nolint:gochecknoglobals // schema enumeration; immutable ordering.
var manifestProbes = []struct {
	filename string
	ptype    projecttype.Type
}{
	{"pom.xml", projecttype.Maven},
	{"package.json", projecttype.NPM},
	{"Cargo.toml", projecttype.Cargo},
	{"go.mod", projecttype.Go},
	{"build.gradle", projecttype.Gradle},
	{"build.gradle.kts", projecttype.Gradle},
}

// AutoDeriveConfig synthesises a Config from a single ecosystem
// manifest at root when no artifacts.yml is present. Returns:
//
//   - cfg, nil  → exactly one root manifest matched; cfg has one
//     Artifact populated from that manifest.
//   - nil, err  → zero or multiple matches. The error message lists
//     every manifest seen so the operator can pick the
//     right action (write artifacts.yml, or remove the
//     stray manifest).
//
// fsys defaults to the OS filesystem rooted at root. Tests pass an
// in-memory fs.FS for determinism.
func AutoDeriveConfig(fsys fs.FS, root string) (*config.Config, error) {
	matches := detectRootManifests(fsys, root)

	switch len(matches) {
	case 0:
		return nil, fmt.Errorf(
			"no .reusable-ci/artifacts.yml and no recognised manifest at repo root\n"+
				"  Looked for: %s\n"+
				"  Either write .reusable-ci/artifacts.yml, or place a single ecosystem manifest at root.\n"+
				"  Docs: docs/artifacts-reference.md: %w",
			manifestProbeNames(), errs.ErrMissingInput,
		)
	case 1:
		return deriveFromManifest(fsys, root, matches[0])
	default:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.filename
		}

		return nil, fmt.Errorf(
			"no .reusable-ci/artifacts.yml and multiple manifests at root: %s\n"+
				"  Cannot auto-derive — a polyglot repo needs an explicit artifacts.yml.\n"+
				"  Docs: docs/artifacts-reference.md: %w",
			strings.Join(names, ", "), errs.ErrInvalidConfig,
		)
	}
}

type detectedManifest struct {
	filename string
	ptype    projecttype.Type
}

func detectRootManifests(fsys fs.FS, root string) []detectedManifest {
	var found []detectedManifest

	for _, probe := range manifestProbes {
		path := filepath.Join(root, probe.filename)
		if statable(fsys, path) {
			found = append(found, detectedManifest{filename: probe.filename, ptype: probe.ptype})
		}
	}
	// build.gradle and build.gradle.kts are two filenames mapping to one
	// project type — collapse so a project with both files (rare but
	// allowed) is counted once.
	return dedupeByType(found)
}

func dedupeByType(in []detectedManifest) []detectedManifest {
	seen := map[projecttype.Type]bool{}
	out := make([]detectedManifest, 0, len(in))

	for _, manifest := range in {
		if seen[manifest.ptype] {
			continue
		}

		seen[manifest.ptype] = true

		out = append(out, manifest)
	}

	return out
}

func statable(fsys fs.FS, path string) bool {
	if fsys == nil {
		return fileExistsOS(path)
	}

	rel := filepath.ToSlash(filepath.Clean(path))
	rel = strings.TrimPrefix(rel, "./")
	rel = strings.TrimPrefix(rel, "/")

	_, err := fs.Stat(fsys, rel)
	// Stat succeeded → the path exists and is a candidate manifest. Any
	// error — ErrNotExist or an unexpected one (permission, symlink loop)
	// — is treated as "no, can't be confident it's a manifest".
	return err == nil
}

func deriveFromManifest(fsys fs.FS, root string, manifest detectedManifest) (*config.Config, error) {
	name, err := readManifestName(fsys, root, manifest)
	if err != nil {
		return nil, fmt.Errorf("auto-derive %s: %w", manifest.filename, err)
	}

	if name == "" {
		return nil, fmt.Errorf(
			"auto-derive %s: could not extract project name; write .reusable-ci/artifacts.yml explicitly: %w",
			manifest.filename, errs.ErrInvalidConfig,
		)
	}

	cfg := &config.Config{
		Artifacts: []config.Artifact{
			{
				Name:             name,
				ProjectType:      manifest.ptype,
				WorkingDirectory: ".",
			},
		},
	}

	// Ecosystems with mandatory typed sub-configs get an empty struct
	// so downstream code sees the consistent "exactly one is non-nil"
	// invariant. Other project types either don't have a typed
	// sub-config or aren't reachable from auto-derive (Meta has no
	// manifest; Python isn't wired yet).
	switch manifest.ptype {
	case projecttype.Cargo:
		cfg.Artifacts[0].Cargo = &config.CargoConfig{}
	case projecttype.Go:
		cfg.Artifacts[0].Go = &config.GoConfig{}
	default:
		// Maven / NPM / Gradle / GradleAndroid / XcodeIOS / Python /
		// Meta / Auto / Unknown — no implicit sub-config needed.
	}

	return cfg, nil
}

// readManifestName reads the manifest body and extracts the
// canonical project name. Each ecosystem has its own one-line
// extractor; we reuse the existing sbom + build domain parsers.
func readManifestName(fsys fs.FS, root string, manifest detectedManifest) (string, error) {
	body, err := readManifest(fsys, root, manifest.filename)
	if err != nil {
		return "", err
	}

	switch manifest.ptype {
	case projecttype.Maven:
		return mavenArtifactID(body)
	case projecttype.NPM:
		return sbom.PackageJSONName(body), nil
	case projecttype.Cargo:
		return sbom.CargoTOMLName(body), nil
	case projecttype.Go:
		return sbom.GoModuleName(body), nil
	case projecttype.Gradle:
		return gradleRootProjectName(fsys, root), nil
	default:
		// GradleAndroid / XcodeIOS / Python / Meta / Auto / Unknown
		// don't reach this function — readManifestName is only called
		// from manifest auto-detection, which only fires for the
		// ecosystems listed in detectedManifest's pattern table.
		return "", nil
	}
}

// mavenArtifactID extracts <artifactId> from a pom.xml body.
func mavenArtifactID(body []byte) (string, error) {
	pom, err := build.ParsePOM(body)
	if err != nil {
		return "", err
	}

	return pom.ArtifactID, nil
}

// gradleRootProjectName tries to read `rootProject.name` from
// settings.gradle{,.kts}, falling back to the directory basename
// when no settings file is present (build.gradle by itself doesn't
// carry a project name).
func gradleRootProjectName(fsys fs.FS, root string) string {
	for _, name := range []string{"settings.gradle", "settings.gradle.kts"} {
		if settings, err := readManifest(fsys, root, name); err == nil {
			if found := sbom.GradleRootProjectName(settings); found != "" {
				return found
			}
		}
	}

	abs, _ := filepath.Abs(root)

	return filepath.Base(abs)
}

func readManifest(fsys fs.FS, root, filename string) ([]byte, error) {
	path := filepath.Join(root, filename)
	if fsys == nil {
		return readFileOS(path)
	}

	rel := filepath.ToSlash(filepath.Clean(path))
	rel = strings.TrimPrefix(rel, "./")
	rel = strings.TrimPrefix(rel, "/")

	return fs.ReadFile(fsys, rel)
}

func manifestProbeNames() string {
	names := make([]string, len(manifestProbes))
	for i, p := range manifestProbes {
		names[i] = p.filename
	}

	return strings.Join(names, ", ")
}
