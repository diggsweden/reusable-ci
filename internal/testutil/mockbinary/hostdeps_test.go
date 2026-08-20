// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package mockbinary_test

import (
	"os/exec"
	"testing"
)

// mockbinary is the most widely used double in the suite -- 29 test
// files across 23 packages -- and it is the one piece of test
// infrastructure that is NOT self-contained. Each stub is a bash script
// that builds its JSON recording by shelling out to jq:
//
//	printf '"args":%s,' "$(printf '%s\n' "$@" | jq -R . | jq -s -c .)"
//
// So every one of those packages needs `bash` and `jq` on the host PATH.
// Neither is declared: not in .mise.toml, not in docs/testing.md, not in
// the justfile. They are present in the runtime image, which is why this
// has never been noticed, and absent from a plain golang:alpine.
//
// Without jq the stub writes `{"name":,"args":,...}` and Invocations
// fails with
//
//	mockbinary: parse line 1: invalid character ',' looking for beginning of value
//
// which names neither jq nor bash. The failure is at least loud rather
// than silent -- Invocations calls t.Fatalf on a malformed line, so no
// assertion can pass vacuously against an empty recording -- and that is
// worth keeping.
//
// This test states the dependency where a maintainer will find it, so
// the next person to hit it reads "jq is required" rather than a JSON
// parse error. Recorded in docs/open-questions.md ("The most-used test
// double depends on undeclared host binaries").

// TestMockbinary_RequiresItsHostDependencies names what the helper needs
// before any package built on it fails obscurely.
func TestMockbinary_RequiresItsHostDependencies(t *testing.T) {
	t.Parallel()

	for _, dep := range []struct {
		bin  string
		what string
	}{
		{bin: "bash", what: "every stub is a bash script (#!/usr/bin/env bash)"},
		{bin: "jq", what: "each stub builds its JSON recording with jq"},
	} {
		if _, err := exec.LookPath(dep.bin); err != nil {
			t.Errorf("mockbinary needs %s on PATH: %s.\n"+
				"Without it the recordings are malformed and every test using this "+
				"helper fails with a JSON parse error that does not name %s.",
				dep.bin, dep.what, dep.bin)
		}
	}
}
