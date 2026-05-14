// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package version

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// UpdatePropertyResult tags whether the line was rewritten in place
// (Updated) or appended at the end (Added). The bash logged both cases
// distinctly so callers can mirror that.
type UpdatePropertyResult int

const (
	UpdatePropertyUpdated UpdatePropertyResult = iota
	UpdatePropertyAdded
)

// UpdateOrAddProperty rewrites the first line that starts with `key`
// (case-sensitive, anchored) to `key<sep><value>`, or appends a new
// line if no match. Mirrors the bash `update_or_add_property` helper.
//
// Returns the new body, a result tag, and whether any change was made
// (true unless the key already had exactly that value).
func UpdateOrAddProperty(body, key, value, sep string) (string, UpdatePropertyResult) {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, key) {
			// Match the bash's anchored `^${key}` — accept any tail after
			// the key prefix. The bash overwrites the whole line with
			// `${key}${sep}${value}`.
			lines[i] = key + sep + value
			return strings.Join(lines, "\n"), UpdatePropertyUpdated
		}
	}
	// Append. Preserve the body's trailing newline shape: if the input
	// ends with "\n" the appended line ends with "\n" too.
	suffix := key + sep + value + "\n"
	if !strings.HasSuffix(body, "\n") && body != "" {
		suffix = "\n" + suffix
	}
	return body + suffix, UpdatePropertyAdded
}

// IncrementVersionCodeResult reports whether the existing versionCode
// was incremented (with old / new) or whether a new versionCode=1
// line was appended.
type IncrementVersionCodeResult struct {
	Body  string
	Old   int
	New   int
	Added bool // true → "versionCode=1" appended
}

var versionCodeLine = regexp.MustCompile(`(?m)^versionCode=([^\s]*)`)

// IncrementVersionCode reads the first `versionCode=N` line from body,
// rewrites it with N+1, and returns the new body. When no
// `versionCode=` line exists, appends `versionCode=1`.
//
// Mirrors the bash `increment_version_code` helper. A non-numeric value
// is treated as 0 (the bash's `tr -d ' '` + arithmetic would fail on
// non-numerics; this is a stricter, defined behaviour).
func IncrementVersionCode(body string) IncrementVersionCodeResult {
	m := versionCodeLine.FindStringSubmatchIndex(body)
	if m == nil {
		// Append.
		out, _ := UpdateOrAddProperty(body, "versionCode", "1", "=")
		return IncrementVersionCodeResult{Body: out, Old: 0, New: 1, Added: true}
	}
	rawValue := body[m[2]:m[3]]
	cur, _ := strconv.Atoi(strings.TrimSpace(rawValue))
	next := cur + 1

	out := body[:m[0]] + "versionCode=" + strconv.Itoa(next) + body[m[1]:]
	return IncrementVersionCodeResult{Body: out, Old: cur, New: next}
}

var gradleJVMVersionLine = regexp.MustCompile(`(?m)^version=.*`)

// UpdateGradleJVMVersion rewrites the first `^version=` line to the
// given version, or appends one if none exists. Mirrors the JVM gradle
// branch of bump-version.sh — strictly anchored on `version=` (with
// separator) so `versionName=` / `versionCode=` are not affected.
func UpdateGradleJVMVersion(body, version string) (string, UpdatePropertyResult) {
	if gradleJVMVersionLine.MatchString(body) {
		return gradleJVMVersionLine.ReplaceAllString(body, "version="+version), UpdatePropertyUpdated
	}
	suffix := "version=" + version + "\n"
	if !strings.HasSuffix(body, "\n") && body != "" {
		suffix = "\n" + suffix
	}
	return body + suffix, UpdatePropertyAdded
}

// UpdateXcodeMarketingVersion rewrites the first
// `^MARKETING_VERSION = ` line to the new value, or appends a new line
// when no match. Used for versions.xcconfig.
func UpdateXcodeMarketingVersion(body, version string) (string, UpdatePropertyResult) {
	return UpdateOrAddProperty(body, "MARKETING_VERSION", version, " = ")
}

// CargoSection identifies which Cargo.toml section drives the version.
// Workspaces win when both [workspace.package] and [package] exist.
type CargoSection int

const (
	CargoSectionNone CargoSection = iota
	CargoSectionWorkspacePackage
	CargoSectionPackage
)

var (
	cargoWorkspaceHeader = "[workspace.package]"
	cargoPackageHeader   = "[package]"
	cargoVersionLine     = regexp.MustCompile(`(?m)^version[[:space:]]*=.*`)
)

// UpdateCargoVersion finds the active version section ([workspace.package]
// preferred, [package] as fallback) and rewrites the first `version =`
// line within that section's body. Mirrors the sed range expressions in
// bump-version.sh's cargo branch.
//
// Returns the section that was rewritten (NotFound when neither header
// is present) and the new body.
func UpdateCargoVersion(body, version string) (string, CargoSection, error) {
	section := CargoSectionNone
	switch {
	case strings.Contains(body, cargoWorkspaceHeader):
		section = CargoSectionWorkspacePackage
	case strings.Contains(body, cargoPackageHeader):
		section = CargoSectionPackage
	default:
		return body, CargoSectionNone, errors.New("Cargo.toml has neither [package] nor [workspace.package] sections")
	}

	header := cargoWorkspaceHeader
	if section == CargoSectionPackage {
		header = cargoPackageHeader
	}

	// Find the section's body range: [start of section line ... start of
	// next "[" line], matching the sed `/header/,/^\[/` range.
	start := strings.Index(body, header)
	// Skip past the header line.
	headerEnd := start + len(header)
	if nl := strings.IndexByte(body[headerEnd:], '\n'); nl >= 0 {
		headerEnd += nl + 1
	} else {
		// Header is the last line — nothing to rewrite.
		return body, section, fmt.Errorf("section %s has no body", header)
	}

	// Find the next "[" at line start.
	rest := body[headerEnd:]
	endOffset := len(rest)
	for i := range len(rest) {
		if rest[i] == '\n' && i+1 < len(rest) && rest[i+1] == '[' {
			endOffset = i + 1
			break
		}
	}
	sectionBody := rest[:endOffset]
	tail := rest[endOffset:]

	newSection := cargoVersionLine.ReplaceAllString(sectionBody,
		fmt.Sprintf(`version = "%s"`, version))

	return body[:headerEnd] + newSection + tail, section, nil
}
