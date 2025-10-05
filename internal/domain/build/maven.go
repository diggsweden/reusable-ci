// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package build holds pure helpers shared by the toolchain build wrappers
// (maven, gradle, npm, gradle-android, xcode-ios).
package build

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/vifraa/gopom"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
)

// IsSnapshot reports whether a Maven version string is a -SNAPSHOT.
// Mirrors check `[[ $VERSION == *-SNAPSHOT ]]`.
func IsSnapshot(version string) bool {
	return strings.HasSuffix(version, "-SNAPSHOT")
}

// POM is the subset of pom.xml fields the build/sbom use cases consume.
//
// Maven inheritance: when a child POM omits version/groupId, they're
// inherited from <parent>. The parser surfaces both so callers can apply
// the inheritance fall-through.
type POM struct {
	GroupID    string
	ArtifactID string
	Version    string
	Parent     POMParent
}

// POMParent is the inheritance source for omitted top-level fields.
type POMParent struct {
	GroupID    string
	ArtifactID string
	Version    string
}

// ParsePOM extracts the typed fields the build/sbom use cases need from a
// pom.xml. The complete XML document envelope is checked before
// github.com/vifraa/gopom extracts the typed fields; this is not Maven schema
// validation.
//
// On top of the raw parse we apply Maven's inheritance rule: when
// <groupId>/<version> is omitted at the top level, the value from <parent>
// is used. ArtifactID is never inherited.
//
// Property interpolation (<version>${revision}</version>) is NOT resolved
// here — the caller decides whether to fall back to `mvn help:evaluate`
// when POMHasUnresolvedProperty(field) reports true. ~95% of POMs use
// literal values; the fallback path stays available for the rest.
func ParsePOM(body []byte) (POM, error) { //nolint:cyclop,gocognit // bounded XML envelope state machine followed by typed extraction and parent fallback.
	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))
	// xml.Unmarshal (used by gopom) stops at the first root. Scan through EOF
	// so valid coordinates cannot hide a second root or malformed suffix.
	decoder := xml.NewDecoder(bytes.NewReader(body))
	depth, seenRoot := 0, false
	firstToken, seenDoctype := true, false

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return POM{}, fmt.Errorf("parse pom.xml: %w: %w", err, errs.ErrInvalidConfig)
		}

		switch node := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				if seenRoot {
					return POM{}, fmt.Errorf("parse pom.xml: multiple root elements: %w", errs.ErrInvalidConfig)
				}

				seenRoot = true
			}

			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && len(bytes.Trim(node, " \t\r\n")) != 0 {
				return POM{}, fmt.Errorf("parse pom.xml: text outside root element: %w", errs.ErrInvalidConfig)
			}
		case xml.ProcInst:
			if strings.EqualFold(node.Target, "xml") && (node.Target != "xml" || !firstToken) {
				return POM{}, fmt.Errorf("parse pom.xml: XML declaration must be lowercase and first: %w", errs.ErrInvalidConfig)
			}
		case xml.Directive:
			kindEnd := bytes.IndexAny(node, " \t\r\n")
			if kindEnd != len("DOCTYPE") || string(node[:kindEnd]) != "DOCTYPE" || len(bytes.Trim(node[kindEnd:], " \t\r\n")) == 0 {
				return POM{}, fmt.Errorf("parse pom.xml: unsupported directive: %w", errs.ErrInvalidConfig)
			}

			if seenRoot || seenDoctype {
				return POM{}, fmt.Errorf("parse pom.xml: DOCTYPE must occur once before the root: %w", errs.ErrInvalidConfig)
			}
			// The DTD stays opaque; neither decoder resolves external references.
			seenDoctype = true
		}

		firstToken = false
	}

	if !seenRoot {
		return POM{}, fmt.Errorf("parse pom.xml: missing root element: %w", errs.ErrInvalidConfig)
	}

	project, err := gopom.ParseFromReader(bytes.NewReader(body))
	if err != nil {
		// A pom.xml that will not parse is the adopter's project
		// configuration, so it exits EX_CONFIG (78). No caller classifies
		// this on our behalf — two propagate it verbatim and one swallows it
		// — so an unclassified error here reached the operator as
		// EX_SOFTWARE (70), "file a bug".
		return POM{}, fmt.Errorf("parse pom.xml: %w: %w", err, errs.ErrInvalidConfig)
	}

	pom := POM{
		GroupID:    derefString(project.GroupID),
		ArtifactID: derefString(project.ArtifactID),
		Version:    derefString(project.Version),
	}
	if project.Parent != nil {
		pom.Parent = POMParent{
			GroupID:    derefString(project.Parent.GroupID),
			ArtifactID: derefString(project.Parent.ArtifactID),
			Version:    derefString(project.Parent.Version),
		}
	}
	// Apply inheritance for omitted child fields. ArtifactID is never
	// inherited — every POM has its own.
	if pom.GroupID == "" {
		pom.GroupID = pom.Parent.GroupID
	}

	if pom.Version == "" {
		pom.Version = pom.Parent.Version
	}

	return pom, nil
}

// derefString returns the trimmed string a points at, or "" if a is nil.
// gopom uses *string for every optional XML element; this normalises into
// the value shape the rest of the domain consumes.
func derefString(a *string) string {
	if a == nil {
		return ""
	}

	return strings.TrimSpace(*a)
}

// POMHasUnresolvedProperty reports whether value carries a Maven property
// reference (${name}) that ParsePOM intentionally does not expand.
// Callers use this to decide whether to fall back to `mvn help:evaluate`.
func POMHasUnresolvedProperty(value string) bool {
	return strings.Contains(value, "${")
}

// MavenSummaryInput drives RenderMavenSummary.
type MavenSummaryInput struct {
	BuildType   string
	GroupID     string
	ArtifactID  string
	Version     string
	JavaVersion string
	SkipTests   bool
	IsSnapshot  bool
}

// RenderMavenSummary returns the markdown block written by.
func RenderMavenSummary(in MavenSummaryInput, now time.Time) string {
	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "## Maven Build Summary 🔨\n")
	_, _ = fmt.Fprintf(&b, "\n")
	_, _ = fmt.Fprintf(&b, "- **Type:** %s\n", summary.LiteralText(in.BuildType))
	_, _ = fmt.Fprintf(&b, "- **Artifact:** %s\n",
		summary.InlineCode(in.GroupID+":"+in.ArtifactID+":"+in.Version))
	_, _ = fmt.Fprintf(&b, "- **Java:** %s\n", summary.LiteralText(in.JavaVersion))

	if in.SkipTests {
		_, _ = fmt.Fprintf(&b, "- **Tests:** ⊘ Skipped\n")
	} else {
		_, _ = fmt.Fprintf(&b, "- **Tests:** ✓ Executed\n")
	}

	_, _ = fmt.Fprintf(&b, "- **Snapshot:** %t\n", in.IsSnapshot)
	_, _ = fmt.Fprintf(&b, "\n")
	_, _ = fmt.Fprintf(&b, "*Build completed at %s*\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))

	return b.String()
}
