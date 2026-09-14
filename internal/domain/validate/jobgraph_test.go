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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

// TestCheckJobGraph_WaiverBelongsToTheJobWhoseBlockHoldsIt covers where an
// annotation attaches. The scan used to key job blocks on a two-space header
// with nothing after the colon, so a waiver written on the NEXT job's header
// line waived the previous job, and a workflow with four-space job
// indentation had no headers at all, so no waiver ever applied. Blocks now
// come from the parsed headers: a header line, trailing comment included,
// belongs to the job it names.
func TestCheckJobGraph_WaiverBelongsToTheJobWhoseBlockHoldsIt(t *testing.T) {
	t.Parallel()

	const masking = "  producer:\n    if: github.event_name == 'push'\n    runs-on: x\n    steps: []\n  consumer:\n    uses: ./.github/workflows/x.yml\n    if: needs.producer.outputs.ok == 'true'\n"

	for name, tc := range map[string]struct {
		body string
		want int
	}{
		"control": {body: "jobs:\n" + masking, want: 1},
		"waiver on the next job's header line": {
			body: "jobs:\n" + masking + "  other: # job-graph-guard: allow reason=\"other is provably safe\"\n    uses: ./.github/workflows/y.yml\n",
			want: 1,
		},
		"waiver inside the consumer": {
			body: "jobs:\n" + masking + "    # job-graph-guard: allow reason=\"producer always runs on push\"\n",
			want: 0,
		},
		"four-space indentation": {
			body: "jobs:\n    producer:\n        if: github.event_name == 'push'\n        runs-on: x\n        steps: []\n    consumer:\n        uses: ./.github/workflows/x.yml\n        if: needs.producer.outputs.ok == 'true'\n        # job-graph-guard: allow reason=\"producer always runs on push\"\n",
			want: 0,
		},
		"waiver in a later top-level key": {
			body: "jobs:\n" + masking + "env:\n  X: 1 # job-graph-guard: allow reason=\"not a job\"\n",
			want: 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if n := countJobGraph(t, tc.body); n != tc.want {
				t.Errorf("violations = %d, want %d", n, tc.want)
			}
		})
	}
}
