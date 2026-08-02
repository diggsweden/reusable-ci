// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/validate"
)

func countJobGraph(t *testing.T, body string) int {
	t.Helper()

	v, err := validate.CheckJobGraph([]byte(body))
	if err != nil {
		t.Fatalf("CheckJobGraph: %v", err)
	}

	return len(v)
}

func TestCheckJobGraph_AlwaysProducerIsClean(t *testing.T) {
	body := `jobs:
  producer:
    if: ${{ always() }}
    runs-on: ubuntu-24.04
    outputs:
      images: ${{ steps.c.outputs.images }}
    steps: []
  consumer:
    needs: [producer]
    if: ${{ needs.producer.outputs.images != '[]' }}
    uses: ./.github/workflows/sign.yml
`
	if n := countJobGraph(t, body); n != 0 {
		t.Errorf("violations = %d, want 0 (producer always() -> not skippable)", n)
	}
}

func TestCheckJobGraph_SkippableProducerFlagged(t *testing.T) {
	body := `jobs:
  producer:
    if: ${{ github.ref == 'refs/heads/main' }}
    runs-on: ubuntu-24.04
    outputs:
      images: ${{ steps.c.outputs.images }}
    steps: []
  consumer:
    needs: [producer]
    if: ${{ needs.producer.outputs.images != '[]' }}
    uses: ./.github/workflows/sign.yml
`
	if n := countJobGraph(t, body); n != 1 {
		t.Errorf("violations = %d, want 1 (skippable producer feeding a reusable consumer)", n)
	}
}

func TestCheckJobGraph_AcknowledgedIsClean(t *testing.T) {
	body := `jobs:
  producer:
    if: ${{ github.ref == 'refs/heads/main' }}
    runs-on: ubuntu-24.04
    outputs:
      images: ${{ steps.c.outputs.images }}
    steps: []
  consumer:
    needs: [producer]
    # job-graph-guard: allow reason="test acknowledgement"
    if: ${{ needs.producer.outputs.images != '[]' }}
    uses: ./.github/workflows/sign.yml
`
	if n := countJobGraph(t, body); n != 0 {
		t.Errorf("violations = %d, want 0 (acknowledged)", n)
	}
}

func TestCheckJobGraph_NonReusableConsumerIgnored(t *testing.T) {
	// A composite-action consumer (uses: …/action, not a .yml workflow) does not
	// have the masking failure mode.
	body := `jobs:
  producer:
    if: ${{ github.ref == 'refs/heads/main' }}
    runs-on: ubuntu-24.04
    outputs:
      images: ${{ steps.c.outputs.images }}
    steps: []
  consumer:
    needs: [producer]
    runs-on: ubuntu-24.04
    steps:
      - uses: some-org/some-action@v1
        with:
          x: ${{ needs.producer.outputs.images }}
`
	if n := countJobGraph(t, body); n != 0 {
		t.Errorf("violations = %d, want 0 (consumer is not a reusable workflow call)", n)
	}
}

func TestCheckJobGraph_AlwaysConsumerWithReadsSkippable(t *testing.T) {
	// Consumer's own if is always(), but its `with` reads a skippable producer's
	// outputs — still a masking edge.
	body := `jobs:
  producer:
    if: ${{ github.ref == 'refs/heads/main' }}
    runs-on: ubuntu-24.04
    outputs:
      images: ${{ steps.c.outputs.images }}
    steps: []
  consumer:
    needs: [producer]
    if: ${{ always() }}
    uses: ./.github/workflows/sign.yml
    with:
      images: ${{ needs.producer.outputs.images }}
`
	if n := countJobGraph(t, body); n != 1 {
		t.Errorf("violations = %d, want 1 (always() consumer with reads skippable producer)", n)
	}
}
