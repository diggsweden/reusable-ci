// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"strconv"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// Need is one thing a scenario needs from the environment it was pointed at.
//
// Environment facts used to be consulted three different ways: hardcoded
// predicates that could not know the environment, one hand-rolled accessor per
// fact, and late skips buried inside helpers. Each new fact then cost a new
// accessor plus a call-site edit in every scenario that cared — ten of them, for
// the runner question alone — and a scenario that forgot one did not fail. It
// waited out a timeout and reported something else.
//
// So the questions are named, and Requires answers them in one place. Adding a
// fact is a Need plus a case, not an edit everywhere.
type Need int

const (
	// NeedsInRunner is a job this suite can drive on a runner that exists.
	// Two facts on purpose: whether the SUITE implements an in-runner probe
	// for the forge, and whether the ENVIRONMENT deployed a runner for it.
	NeedsInRunner Need = iota

	// NeedsFulcio is a certificate authority that will issue for this forge:
	// one deployed at all, and configured to trust this issuer.
	NeedsFulcio
)

// Requires reports whether kind satisfies every need, logging the first that
// does not.
//
// Logf rather than Skipf: these run inside a loop over forges, where skipping
// the test would abandon the forges after this one. The caller continues.
//
// Each message names the contract field that decides it, so a reader is sent to
// the environment's own description of itself rather than to a symptom. That is
// the whole reason this exists: an unmet need used to surface as a queued job or
// an opaque cosign error, both of which read as product defects.
func Requires(tb TB, kind provider.Platform, needs ...Need) bool {
	tb.Helper()

	for _, need := range needs {
		switch need {
		case NeedsInRunner:
			if !RunsInRunner(kind) {
				tb.Logf("SKIP %s: the in-runner tier does not drive this forge yet", kind)

				return false
			}

			if !RunnerAvailable(kind) {
				tb.Logf("SKIP %s: this environment deployed no runner for it (LAB_RUNNER_FORGES)", kind)

				return false
			}
		case NeedsFulcio:
			if _, ok := FulcioURL(); !ok {
				tb.Logf("SKIP %s: this environment provides no Fulcio (LAB_FULCIO_URL unset)", kind)

				return false
			}

			if !FulcioTrusts(kind) {
				tb.Logf("SKIP %s: this environment's Fulcio is not configured to trust it (LAB_FULCIO_ISSUERS)", kind)

				return false
			}
		default:
			// An unwired Need is a programming error, not a skip: silently
			// treating it as satisfied is how a gate stops gating.
			tb.Fatalf("livetest: unhandled %s; wire it into Requires", need)

			return false
		}
	}

	return true
}

// String names the need, so an unwired one reports what it is rather than which
// integer it happens to be. The diagnostic is the whole reason this file exists;
// it would be poor to make the file's own failure illegible.
func (n Need) String() string {
	switch n {
	case NeedsInRunner:
		return "NeedsInRunner"
	case NeedsFulcio:
		return "NeedsFulcio"
	default:
		return "Need(" + strconv.Itoa(int(n)) + ")"
	}
}

// allNeeds is every Need, so a test can assert Requires handles each one. The
// exhaustiveness guard the default case cannot provide on its own: a new Need
// that nobody wired would otherwise only fail the first time a scenario used it.
func allNeeds() []Need {
	return []Need{NeedsInRunner, NeedsFulcio}
}
