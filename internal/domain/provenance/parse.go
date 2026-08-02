// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provenance

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// checksumLine matches a GoReleaser-format checksum line:
// "<64-hex-sha256>  <relative-path>". Coreutils binary-mode checksums prefix
// the path with "*"; that marker is not part of the subject name.
var checksumLine = regexp.MustCompile(`^([A-Fa-f0-9]{64})\s+\*?(.+)$`)

// ParseChecksums reads GoReleaser-format checksum lines and returns one
// Subject per line (digest lower-cased). Blank lines are skipped; a
// malformed non-blank line is an error, and zero subjects is an error —
// an empty subject set would make the provenance meaningless.
func ParseChecksums(r io.Reader) ([]Subject, error) {
	var subjects []Subject

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}

		m := checksumLine.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("provenance: malformed checksum line %q: %w", line, errs.ErrValidation)
		}

		subjects = append(subjects, Subject{Name: m[2], SHA256: strings.ToLower(m[1])})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("provenance: read checksums: %w", err)
	}

	if len(subjects) == 0 {
		return nil, fmt.Errorf("provenance: no subjects parsed from checksums: %w", errs.ErrUsage)
	}

	return subjects, nil
}

// ParseGoSum reads a go.sum and returns one Dependency per module
// release line, skipping the "/go.mod" hash lines. The URI is a purl
// (pkg:golang/<module>@<version>) and the digest is the Go sumdb h1:
// hash typed as "gomod_h1" (not "sha256" — it is a Merkle-tree hash, so
// naming it precisely tells verifiers what to compare against). Mirrors
// the jq pipeline in slsa-provenance.sh.
func ParseGoSum(r io.Reader) ([]Dependency, error) {
	var deps []Dependency

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}

		module, version, hash := fields[0], fields[1], fields[2]
		if strings.HasSuffix(version, "/go.mod") {
			continue
		}

		deps = append(deps, Dependency{
			URI:        "pkg:golang/" + module + "@" + strings.ReplaceAll(version, "+", "%2B"),
			DigestType: "gomod_h1",
			Digest:     strings.TrimSuffix(strings.TrimPrefix(hash, "h1:"), "="),
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("provenance: read go.sum: %w", err)
	}

	return deps, nil
}

// SourceDependency returns the git-source resolvedDependency entry
// (uri git+<repo>@<ref>, digest gitCommit=<sha>) that leads the
// resolvedDependencies list.
func SourceDependency(repositoryURL, ref, sha string) Dependency {
	return Dependency{
		URI:        "git+" + repositoryURL + "@" + ref,
		DigestType: "gitCommit",
		Digest:     sha,
	}
}

// BaseImageDependency returns the resolvedDependency entry for the base image a
// container was built FROM: uri=<ref>, digest sha256=<digest hex>, annotated
// role="base-image". This is the SLSA-standard expression of base-image lineage
// — a verifier reads it with `cosign verify-attestation --type slsaprovenance1`,
// so no bespoke predicate type is required. The digest is the cryptographic
// truth of which base was used; pass it without the "sha256:" prefix.
func BaseImageDependency(ref, digest string) Dependency {
	return Dependency{
		URI:         ref,
		DigestType:  "sha256",
		Digest:      strings.TrimPrefix(digest, "sha256:"),
		Annotations: map[string]string{"role": "base-image"},
	}
}
