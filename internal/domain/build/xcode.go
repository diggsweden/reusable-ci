// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// XcodeVersionInfo is the (marketing-version, build-number) pair the
// xcode build pipeline emits. Both default to "unknown" when missing.
type XcodeVersionInfo struct {
	Version string
	Build   string
}

// xcodePbxKeyValue matches "<KEY> = <value>;" pairs in
// project.pbxproj. The bash uses `awk -F' = '` then `tr -d ';'` —
// this regex captures the same shape and tolerates leading whitespace.
var xcodePbxKeyValue = regexp.MustCompile(`^\s*([A-Z_][A-Z0-9_]*)\s*=\s*([^;]+);`)

// ParseXcodeVersionFromPbxproj extracts MARKETING_VERSION and
// CURRENT_PROJECT_VERSION from a project.pbxproj body. Both default to
// "unknown" when not found, mirroring the bash fallback.
//
// The pbxproj format is undocumented but stable enough that grep-style
// extraction works in practice; this helper preserves the bash shape
// while typing the result.
func ParseXcodeVersionFromPbxproj(body string) XcodeVersionInfo {
	out := XcodeVersionInfo{Version: "unknown", Build: "unknown"} //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.

	for _, line := range strings.Split(body, "\n") {
		m := xcodePbxKeyValue.FindStringSubmatch(line) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if m == nil {
			continue
		}

		key := m[1]
		val := strings.TrimSpace(strings.Trim(m[2], `"`))

		switch key {
		case "MARKETING_VERSION":
			if out.Version == "unknown" {
				out.Version = val
			}
		case "CURRENT_PROJECT_VERSION":
			if out.Build == "unknown" {
				out.Build = val
			}
		}

		if out.Version != "unknown" && out.Build != "unknown" {
			return out
		}
	}

	return out
}

// XcodeSummaryInput drives RenderXcodeSummary.
type XcodeSummaryInput struct {
	XcodeVersion  string
	Scheme        string
	Configuration string
	Destination   string
	Signing       bool
	Version       string
	BuildNumber   string
	IPAName       string
}

// RenderXcodeSummary returns the markdown block written by.
func RenderXcodeSummary(in XcodeSummaryInput, now time.Time) string {
	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "## Xcode Build Summary 📱\n\n")
	_, _ = fmt.Fprintf(&b, "### Configuration\n")
	_, _ = fmt.Fprintf(&b, "| Setting | Value |\n")
	_, _ = fmt.Fprintf(&b, "|---------|-------|\n")
	_, _ = fmt.Fprintf(&b, "| **Xcode** | %s |\n", in.XcodeVersion)
	_, _ = fmt.Fprintf(&b, "| **Scheme** | %s |\n", in.Scheme)
	_, _ = fmt.Fprintf(&b, "| **Configuration** | %s |\n", in.Configuration)
	_, _ = fmt.Fprintf(&b, "| **Destination** | %s |\n", in.Destination)
	_, _ = fmt.Fprintf(&b, "| **Signing** | %s |\n", boolStatus(in.Signing))

	if in.Version != "" && in.Version != "unknown" {
		_, _ = fmt.Fprintf(&b, "| **Version** | %s (%s) |\n", in.Version, in.BuildNumber)
	}

	_, _ = fmt.Fprintf(&b, "\n### Artifacts Generated\n")

	if in.Signing {
		_, _ = fmt.Fprintf(&b, "✓ IPA: `%s`\n", in.IPAName)
	} else {
		_, _ = fmt.Fprintf(&b, "✓ Archive: `%s-archive`\n", in.IPAName)
	}

	_, _ = fmt.Fprintf(&b, "\n*Build completed at %s*\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))

	return b.String()
}
