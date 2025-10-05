// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// DeclaredSubject is what a CycloneDX document says it describes. A document
// without a metadata component is an aggregate: it inventories a tree rather
// than naming one released thing, and has no subject to compare against.
type DeclaredSubject struct {
	Name      string
	Version   string
	Aggregate bool
}

// declaredDocument is the sliver of CycloneDX this needs. Everything else in
// the document is passed through untouched; nothing here rewrites a BOM.
type declaredDocument struct {
	BOMFormat string `json:"bomFormat"`
	Metadata  *struct {
		Component *struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"component"`
	} `json:"metadata"`
}

// ReadDeclaredSubject parses what a harvested CycloneDX BOM claims to describe.
// A document that is not CycloneDX at all is refused: a build layer named after
// a release must at least be the format the name promises.
func ReadDeclaredSubject(body []byte) (DeclaredSubject, error) {
	var doc declaredDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return DeclaredSubject{}, fmt.Errorf("parse harvested CycloneDX BOM: %w: %w", err, errs.ErrMalformedInput)
	}

	if !strings.EqualFold(doc.BOMFormat, "CycloneDX") {
		return DeclaredSubject{}, fmt.Errorf("harvested BOM is not CycloneDX (bomFormat %q): %w", doc.BOMFormat, errs.ErrMalformedInput)
	}

	if doc.Metadata == nil || doc.Metadata.Component == nil {
		return DeclaredSubject{Aggregate: true}, nil
	}

	return DeclaredSubject{Name: doc.Metadata.Component.Name, Version: doc.Metadata.Component.Version}, nil
}

// SameRelease reports whether two version strings name the same release, and
// whether they were comparable at all.
//
// Comparison is on the release core only. A build stamps the version its own
// ecosystem uses, so the same release legitimately appears as 3.0.0, 3.0.0-rc.1
// and 3.0.0-SNAPSHOT depending on which tool wrote the BOM, and a leading "v"
// is a tag spelling rather than a different version. What it does catch is the
// case this binding exists for: a BOM harvested from a different module or an
// earlier release, whose core differs outright.
//
// Anything that is not a recognizable MAJOR.MINOR.PATCH core on both sides is
// not comparable, and an unknown quantity is not a mismatch.
func SameRelease(left, right string) (bool, bool) {
	leftCore, leftOK := releaseCore(left)

	rightCore, rightOK := releaseCore(right)
	if !leftOK || !rightOK {
		return false, false
	}

	return leftCore == rightCore, true
}

// releaseCore extracts MAJOR.MINOR.PATCH, dropping a leading "v" and any
// pre-release or build metadata after it.
func releaseCore(version string) (string, bool) {
	core := strings.TrimPrefix(strings.TrimSpace(version), "v")
	for _, cut := range []string{"-", "+"} {
		core, _, _ = strings.Cut(core, cut)
	}

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return "", false
	}

	for _, part := range parts {
		if part == "" || strings.TrimLeft(part, "0123456789") != "" {
			return "", false
		}
	}

	return core, true
}
