// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container

import "regexp"

// RebuildPatterns is the list of regex patterns the bash uses to
// detect "Containerfile rebuilds from source" warnings. Any match
// triggers an advisory warning that pre-built artifacts will be
// ignored.
//
// Mirrors the patterns in scripts/container/validate-artifacts.sh
// (`mvn package`, `mvn install`, `mvnw package/install`, `gradle build`,
// `gradle assemble`, `npm run build`, `npm ... build run`).
var RebuildPatterns = []*regexp.Regexp{
	regexp.MustCompile(`mvn.*package`),
	regexp.MustCompile(`mvn.*install`),
	regexp.MustCompile(`mvnw.*package`),
	regexp.MustCompile(`mvnw.*install`),
	regexp.MustCompile(`gradle.*build`),
	regexp.MustCompile(`gradle.*assemble`),
	regexp.MustCompile(`npm run build`),
	regexp.MustCompile(`npm.*build.*run`),
}

// ContainerfileRebuildsFromSource returns true when the given
// Containerfile body matches any of RebuildPatterns. Pure helper —
// callers handle the file I/O and warning emission.
func ContainerfileRebuildsFromSource(body string) bool {
	for _, p := range RebuildPatterns {
		if p.MatchString(body) {
			return true
		}
	}
	return false
}
