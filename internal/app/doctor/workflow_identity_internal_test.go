// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package doctor

import (
	"fmt"
	"testing"
)

func TestWorkflowIdentityBoundary_ParsedUsesOnly(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		uses string
		want bool
	}{
		{"Org/Engine/.github/workflows/release.yml@main", true}, {"org/engine/action@master", true},
		{"https://forge.example/org/engine/.forgejo/workflows/release.yml@main", true},
		{"other-org/engine/action@main", false}, {"prefixorg/engine/action@main", false}, {"org/engine-suffix/action@main", false},
		{"org/engine/action@main-extra", false}, {"org/engine/action@MAIN", false}, {"org/engine/action@v1.2.3", false},
		{"./org/engine@main", false}, {"docker://org/engine@main", false}, {"org/engine/../action@main", false},
	} {
		for _, step := range []bool{false, true} {
			body := fmt.Sprintf("jobs:\n  release:\n    uses: %q\n", tc.uses)
			if step {
				body = fmt.Sprintf("jobs:\n  release:\n    steps:\n      - uses: %q\n", tc.uses)
			}

			got, err := hasFloatingReusableCIRef([]byte(body), "org/engine")
			if err != nil || got != tc.want {
				t.Fatalf("uses=%s step=%v got=%v err=%v", tc.uses, step, got, err)
			}
		}
	}

	for _, body := range []string{"# uses: org/engine/action@main\njobs: {}\n", "jobs:\n  release:\n    steps:\n      - run: 'uses: org/engine/action@main'\n"} {
		if got, err := hasFloatingReusableCIRef([]byte(body), "org/engine"); err != nil || got {
			t.Fatalf("inert text matched: %v %v", got, err)
		}
	}
}
