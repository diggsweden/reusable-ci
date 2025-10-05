// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package syncguard

import (
	"bytes"
	"crypto/sha256"
	"fmt"
)

func generatedDifference(path, refresh string, actual, expected []byte) string {
	if bytes.Equal(actual, expected) {
		return ""
	}

	index := 0
	for index < len(actual) && index < len(expected) && actual[index] == expected[index] {
		index++
	}

	start := max(0, index-40)

	return fmt.Sprintf("%s is out of sync; refresh with %s\nactual: %d bytes sha256=%x\nexpected: %d bytes sha256=%x\nfirst difference at byte %d\nactual context: %q\nexpected context: %q", path, refresh, len(actual), sha256.Sum256(actual), len(expected), sha256.Sum256(expected), index, actual[start:min(len(actual), index+80)], expected[start:min(len(expected), index+80)])
}
