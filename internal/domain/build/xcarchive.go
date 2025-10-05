// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// ArchiveSchemeName reads the scheme an .xcarchive records it was built with,
// from the bundle's Info.plist.
//
// The release flow selects a scheme, passes it to xcodebuild, and then
// publishes whatever appeared at the archive path. The argv is asserted exactly
// and a pre-existing archive is refused, so the bundle provably came from this
// run's invocation — but nothing had ever read what the bundle itself says it
// is. An xcodebuild that resolved the scheme differently, or a project whose
// scheme list changed underneath the selection, would produce an archive for
// something other than the release being cut, and it would be signed and
// published without a word.
//
// Two return shapes, and the difference is deliberate:
//
//   - ("", nil) means the document parsed and carries no SchemeName. The caller
//     reports the identity as unverified rather than refusing, because an
//     archive without that key is not evidence of the wrong scheme.
//   - an error means the document is not something this can read at all — a
//     binary plist, or not a plist. That is reported, not refused, for the same
//     reason: it says nothing about which scheme was used.
//
// A refusal belongs to the caller and only on a MISMATCH, which is the one case
// that is evidence of a real problem.
func ArchiveSchemeName(body []byte) (string, error) {
	if bytes.HasPrefix(body, []byte("bplist")) {
		return "", fmt.Errorf("archive Info.plist is a binary plist, which this cannot read: %w", errs.ErrUnsupported)
	}

	var doc struct {
		Keys   []string `xml:"dict>key"`
		Values []string `xml:"dict>string"`
	}

	if err := xml.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("parse archive Info.plist: %w: %w", err, errs.ErrMalformedInput)
	}

	// A plist dict is a flat alternating key/value sequence, and xml.Unmarshal
	// collects the two element kinds separately. Pairing them by index is only
	// correct while every value is a string; a nested dict or array would shift
	// the alignment, so a key whose value is not a string ends the scan rather
	// than reading the wrong one.
	for index, key := range doc.Keys {
		if key != "SchemeName" {
			continue
		}

		if index >= len(doc.Values) {
			return "", nil
		}

		return strings.TrimSpace(doc.Values[index]), nil
	}

	return "", nil
}
