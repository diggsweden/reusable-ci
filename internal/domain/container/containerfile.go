// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import "regexp"

// RebuildPatterns detects "Containerfile rebuilds from source" warnings. Any
// match triggers an advisory warning that pre-built artifacts will be ignored.
// (`mvn package`, `mvn install` -- which also cover `./mvnw` -- `gradle build`,
// `gradle assemble`, `npm run build`, an npm build step followed by an npm run
// such as `npm --workspace web build && npm run start`, `go build`).
//
// The list used to carry separate mvnw entries. They could never match
// anything the mvn entries did not, since every "mvnw" contains "mvn", so they
// were dead and are gone. This is a substring heuristic over the whole file,
// comments included; a match does not prove the Containerfile executes a
// build, only that it mentions one.
//
//nolint:gochecknoglobals // precompiled regex table — read-only.
var RebuildPatterns = []*regexp.Regexp{
	regexp.MustCompile(`mvn.*package`),
	regexp.MustCompile(`mvn.*install`),
	regexp.MustCompile(`gradle.*build`),
	regexp.MustCompile(`gradle.*assemble`),
	regexp.MustCompile(`npm run build`),
	regexp.MustCompile(`npm.*build.*run`),
	regexp.MustCompile(`go build`),
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
