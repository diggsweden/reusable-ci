// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
)

func FuzzEnrichGitHubSARIF(f *testing.F) {
	seeds := [][]byte{
		[]byte(`{}`),
		[]byte(`[]`),
		[]byte(`{"runs":[{"results":[{"ruleId":"RULE","message":{"text":"boom"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"a.go"},"region":{"startLine":12}}}],"fingerprints":{"matchBasedId/v1":"match-xyz"}}]}]}`),
		[]byte(`{"runs":[{"results":[{}]}]}`),
		[]byte(`{"runs":[{"results":[{"partialFingerprints":{"primaryLocationLineHash":"preserved"}}]}]}`),
		[]byte(`[1,2,3]`),
		[]byte(`not json`),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, body []byte) {
		out, err := security.EnrichGitHubSARIF(body)
		if err != nil {
			return
		}

		var doc any
		if jsonErr := json.Unmarshal(out, &doc); jsonErr != nil {
			t.Fatalf("output is not valid JSON: %v\n%s", jsonErr, out)
		}

		out2, err := security.EnrichGitHubSARIF(out)
		if err != nil {
			t.Fatalf("second enrich failed: %v\n%s", err, out)
		}

		if !bytes.Equal(out, out2) {
			t.Fatalf("enrich not idempotent\nfirst:  %s\nsecond: %s", out, out2)
		}
	})
}
