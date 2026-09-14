// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"gopkg.in/yaml.v3"
)

func TestIsolation_SecretTraversalBounds(t *testing.T) {
	t.Parallel()

	for _, kind := range []yaml.Kind{yaml.AliasNode, yaml.SequenceNode} {
		for _, depth := range []int{3, aliasDepthLimit + 1} {
			node := &yaml.Node{Kind: yaml.ScalarNode, Line: 7, Value: "${{ secrets.KEY }}"}

			for range depth {
				if kind == yaml.AliasNode {
					node = &yaml.Node{Kind: kind, Alias: node}
				} else {
					node = &yaml.Node{Kind: kind, Content: []*yaml.Node{node}}
				}
			}

			refs, err := findSecretRefs(node, map[string]bool{"KEY": true})
			if depth > aliasDepthLimit {
				if !errors.Is(err, errs.ErrMalformedInput) || len(refs) != 0 {
					t.Fatalf("kind=%v depth=%d refs=%v err=%v", kind, depth, refs, err)
				}
			} else if err != nil || len(refs) != 1 || refs[0].line != 7 || refs[0].secret != "KEY" {
				t.Fatalf("bounded alias/nesting lost a reference: %v %v", refs, err)
			}
		}
	}
}

func TestIsolation_PrepareScalarAttribution(t *testing.T) {
	t.Parallel()

	body := "jobs:\n  prepare:\n    steps:\n      - run: ${{ secrets.BEFORE }}\n      - uses: actions/checkout@v4\n        with:\n          persist-credentials: false\n      - run: >-\n          ${{ secrets.KEY }}\n          ${{ secrets.OTHER }}\n      - env:\n          KEY: ${{ secrets.KEY }}\n"

	got, err := CheckIsolation([]byte(body), IsolationConfig{PrepareJob: "prepare", SigningSecrets: []string{"BEFORE", "KEY", "OTHER"}})
	if err != nil || len(got) != 3 {
		t.Fatalf("violations=%v err=%v", got, err)
	}

	for index, wantLine := range []int{8, 8, 12} {
		if got[index].Line != wantLine || !strings.Contains(got[index].Msg, `prepare job "prepare"`) || !strings.Contains(got[index].Msg, "at/after checkout") || strings.Contains(got[index].Msg, "BEFORE") {
			t.Fatalf("incorrect prepare attribution: %v", got[index])
		}
	}
}
