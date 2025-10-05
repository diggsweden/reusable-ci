// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"fmt"
	"os"
	"time"
)

// Live-test deadlines.
//
// Every wait in this package is against a real forge over a real network, so
// the right value depends on the runner rather than on the code: a three-minute
// CLI deadline that is generous on a laptop is tight on a loaded shared runner,
// and a workflow that normally settles in two minutes can queue for six behind
// someone else's job. Until now each deadline was a literal, so the only way to
// stretch one was to rebuild.
//
// Each is named, carries its current value as the default, and can be
// overridden with RC_LIVE_TIMEOUT_<NAME> as a Go duration string ("8m", "90s"),
// matching the existing RC_LIVE_* convention (see RC_LIVE_KEEP_SCRATCH).
// Nothing here changes a default: this is the same set of values, reachable.
//
// Four literals in this package are deliberately NOT knobs, so the next reader
// does not re-find them as an oversight:
//
//   - allowedClockSkew (livetest.go) is a security tolerance, not a wait.
//     Widening it from the environment would weaken a check.
//   - ReadHeaderTimeout and IdleTimeout (trust.go) configure the harness's own
//     in-process proxy server. They bound clients we control, not a forge.
//   - the 5s poll interval in workflow.go is a sleep between polls; the wait it
//     serves is timeoutWorkflowPoll, which IS configurable.
//   - the 10s dial and 5s probe in trust.go bound a local TCP connect, which a
//     slow forge does not make slower.
const (
	// timeoutAPI bounds one forge or registry API call.
	timeoutAPI = time.Minute
	// timeoutScratch bounds creating or destroying a scratch project, which
	// is several API calls plus the forge's own bookkeeping.
	timeoutScratch = 2 * time.Minute
	// timeoutCLI bounds one reusable-ci invocation driven by the harness.
	timeoutCLI = 3 * time.Minute
	// timeoutWorkflowLog bounds fetching a finished run's logs, which can be
	// large.
	timeoutWorkflowLog = 3 * time.Minute
	// timeoutWorkflowPoll bounds the polling loop that waits for a run to
	// reach a terminal state.
	timeoutWorkflowPoll = 4 * time.Minute
	// timeoutWorkflow bounds dispatching a workflow run and waiting on the
	// dispatch itself.
	timeoutWorkflow = 5 * time.Minute
	// timeoutHTTPShort bounds a single short HTTP request where the harness
	// is probing rather than waiting.
	timeoutHTTPShort = 30 * time.Second
)

// timeoutEnvPrefix matches the package's existing RC_LIVE_* env convention.
const timeoutEnvPrefix = "RC_LIVE_TIMEOUT_"

// timeoutFor resolves a named deadline, honouring RC_LIVE_TIMEOUT_<NAME>.
//
// An unparsable or non-positive override panics rather than falling back to
// the default. An operator who sets RC_LIVE_TIMEOUT_CLI=8min (not a Go
// duration) is trying to change a deadline; silently running with the old one
// reproduces exactly the "the value is not reachable" problem this exists to
// remove, and would do it while looking like it worked.
func timeoutFor(name string, def time.Duration) time.Duration {
	variable := timeoutEnvPrefix + name

	raw, ok := os.LookupEnv(variable)
	if !ok || raw == "" {
		return def
	}

	override, err := time.ParseDuration(raw)
	if err != nil {
		panic(fmt.Sprintf("%s=%q is not a Go duration (e.g. %q): %v", variable, raw, def.String(), err))
	}

	if override <= 0 {
		panic(fmt.Sprintf("%s=%q must be positive", variable, raw))
	}

	return override
}

// The named deadlines. Each reads its own RC_LIVE_TIMEOUT_<NAME>.
func apiTimeout() time.Duration          { return timeoutFor("API", timeoutAPI) }
func scratchTimeout() time.Duration      { return timeoutFor("SCRATCH", timeoutScratch) }
func cliTimeout() time.Duration          { return timeoutFor("CLI", timeoutCLI) }
func workflowTimeout() time.Duration     { return timeoutFor("WORKFLOW", timeoutWorkflow) }
func workflowPollTimeout() time.Duration { return timeoutFor("WORKFLOW_POLL", timeoutWorkflowPoll) }
func workflowLogTimeout() time.Duration  { return timeoutFor("WORKFLOW_LOG", timeoutWorkflowLog) }
func httpShortTimeout() time.Duration    { return timeoutFor("HTTP_SHORT", timeoutHTTPShort) }
