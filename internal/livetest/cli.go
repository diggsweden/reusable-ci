// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// Some of what this tier must prove is not observable from an adapter.
//
// A capability the forge lacks is supposed to *degrade*: SARIF upload emits a
// notice and exits 0, keyless signing emits a warning naming the forge,
// provenance refuses. Every one of those decisions is taken above the adapter,
// in the use case and the CLI, so a scenario that only calls roles cannot see
// any of them — it would be asserting on the layer that does not decide.
//
// So the kit drives the shipped binary too, against the same guarded target.
// Which one a scenario reaches for is not a style choice: assert on a role when
// the claim is about forge state, and on the binary when the claim is about
// what the user is told and what the process exits with.

// binaryEnv names the built product under test. It is outside the
// REUSABLE_CI_* namespace on purpose: that namespace is the product's own
// flags, and a variable that only points a test suite at a binary must not look
// like one of them.
const binaryEnv = "RC_LIVE_BIN"

// Run is one invocation of the product binary.
type Run struct {
	Args     []string
	Stdout   string
	Stderr   string
	ExitCode int
}

// Combined is stdout and stderr together, for assertions that do not care which
// stream carried the message. Prefer asserting on the specific stream: which one
// a message lands on is part of the CLI contract.
func (r Run) Combined() string { return r.Stdout + r.Stderr }

// Binary is the product under test, built and checksummed outside the tests and
// handed over by path. It is never built here: a suite that compiles its own
// binary proves something about the source it happened to see, not about the
// artifact the release flow produces.
func Binary(tb TB) string {
	tb.Helper()

	path := os.Getenv(binaryEnv)
	if !filepath.IsAbs(path) {
		tb.Fatalf("livetest: %s must be the absolute path of the built product; run this through `just test-live`", binaryEnv)
	}

	// G304: the path is the built product handed over by the recipe, and it is
	// required to be absolute and executable before anything runs it.
	info, err := os.Stat(path) //nolint:gosec
	if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		tb.Fatalf("livetest: %s is not an executable file: %s", binaryEnv, path)
	}

	return path
}

// CLI runs the product against a guarded target and returns what the user would
// have seen. A non-zero exit is a result, not a failure: most of what this tier
// asserts about degradation is an exit code plus a message.
func CLI(tb TB, target Target, repo string, args ...string) Run {
	tb.Helper()

	return CLIIn(tb, target, repo, RunOptions{}, args...)
}

// usageErrorMarkers are how the CLI framework reports that it could not parse
// the invocation at all.
//
//nolint:gochecknoglobals // read-only table.
var usageErrorMarkers = []string{
	"flag provided but not defined",
	"for available flags",
	"unknown subcommand",
	"unknown command",
}

// RunOptions adjusts one invocation. Both fields exist because the product is
// right to care about them: several verbs record paths that a consumer resolves
// later, so they insist those paths are relative, which only means something
// against a known working directory.
type RunOptions struct {
	// Dir is the working directory. Empty means the test's own.
	Dir string

	// Env adds to the closed environment, merged last so a scenario can be
	// explicit about what the product sees.
	Env map[string]string

	// AllowUsageError opts out of the parser-rejection guard, for a scenario
	// whose subject IS how the CLI handles a bad invocation.
	AllowUsageError bool
}

// CLIIn is CLI with options, for the verbs that need a working directory or
// extra variables — registry credentials, most of all.
func CLIIn(tb TB, target Target, repo string, opts RunOptions, args ...string) Run {
	tb.Helper()
	requireAccepted(tb, target)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// G204: args are scenario-authored verbs and flags, never external input.
	cmd := exec.CommandContext(ctx, Binary(tb), args...) //nolint:gosec

	env := cliEnv(target, repo)
	for key, value := range opts.Env {
		env = append(env, key+"="+value)
	}

	cmd.Env = env
	cmd.Dir = opts.Dir

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	run := Run{Args: args, Stdout: stdout.String(), Stderr: stderr.String()}
	assertNotAUsageError(tb, run, opts.AllowUsageError)

	var exitErr *exec.ExitError

	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		run.ExitCode = exitErr.ExitCode()
	default:
		tb.Fatalf("livetest: run %q: %v", strings.Join(args, " "), err)
	}

	return run
}

// cliEnv is the whole environment the product sees: the target, plus the few
// runtime variables any process needs.
//
// It is built from nothing rather than inherited. The operator's shell during a
// live run holds a lab token, quite possibly a real GITHUB_TOKEN, and whatever
// CI variables a previous experiment left behind — and this binary's entire job
// is to detect its platform from the environment and then authenticate to it.
// Handing it the ambient environment would mean the suite could not say which
// forge the product talked to, and a stray credential could be sent to a lab
// host. So every variable it can act on is either set here or absent.
func cliEnv(target Target, repo string) []string {
	values := map[string]string{
		// Detection is pinned rather than inferred: this runs on a laptop, and
		// leaving the product to guess would make the result depend on whose
		// shell it was.
		"REUSABLE_CI_PROVIDER": string(target.Kind),
		"REUSABLE_CI_RUNNER":   "local",

		// Containment, for every invocation rather than for the scenarios that
		// remember. cosign publishes to the PUBLIC Rekor log by default, and a
		// Rekor entry is permanent and append-only — a run cannot take one
		// back. Setting it here means no test can publish, including tests
		// nobody has written yet, which is the property a per-scenario setting
		// cannot buy. The engine derives both halves from this one value, so
		// signing writes no entry and verifying stops demanding one.
		"REUSABLE_CI_COSIGN_TRANSPARENCY": "none",
	}

	// The same target mapping the adapters get, so the binary and a role call
	// address the identical instance.
	targetValues := targetEnv(target, repo)
	for _, key := range targetEnvKeys(target) {
		if value := targetValues(key); value != "" {
			values[key] = value
		}
	}

	// Runtime only. HOME and TMPDIR because subprocesses and temp files need
	// them; PATH because the product shells out to git and cosign; the TLS
	// variables because the lab's CA may be trusted through a file rather than
	// the system store.
	for _, key := range []string{"PATH", "HOME", "TMPDIR", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if value := os.Getenv(key); value != "" {
			values[key] = value
		}
	}

	env := make([]string, 0, len(values))
	for key, value := range values {
		env = append(env, key+"="+value)
	}

	return env
}

// targetEnvKeys lists the variables targetEnv answers for a kind, so cliEnv can
// materialise them without the closure leaking its map.
func targetEnvKeys(target Target) []string {
	switch target.Kind {
	case provider.PlatformForgejo:
		return []string{"FORGEJO_TOKEN", "GITEA_TOKEN", "FORGEJO_SERVER_URL", "FORGEJO_REPOSITORY"}
	case provider.PlatformGitLab:
		return []string{"GITLAB_TOKEN", "CI_SERVER_URL", "CI_PROJECT_PATH"}
	case provider.PlatformGitHub, provider.PlatformLocal:
		return nil
	}

	return nil
}

// assertNotAUsageError fails the test when the CLI could not parse the
// invocation, rather than letting a scenario assert against the parser's
// complaint.
//
// That is a scenario bug, not a product result, and it hides well: a usage error
// exits non-zero and its text names the command, so a check for "it failed and
// said something about auth" passes on it. PAR-TOK-3 did exactly that from the
// day it was written — it drove `validate auth tokens`, which does not exist,
// and matched "auth" inside "Run 'reusable-ci validate auth --help'". It
// reported a covered surface that was never exercised.
//
// Enforced in the kit rather than per scenario so it also covers the ones nobody
// has written yet. A scenario whose subject IS how the CLI handles a bad
// invocation opts out with RunOptions.AllowUsageError.
func assertNotAUsageError(tb TB, run Run, allow bool) {
	tb.Helper()

	if allow {
		return
	}

	lower := strings.ToLower(run.Stderr)
	for _, marker := range usageErrorMarkers {
		if strings.Contains(lower, marker) {
			tb.Fatalf("livetest: %q was rejected by the argument parser, so this scenario exercised the CLI rather than the forge\nstderr: %s",
				strings.Join(run.Args, " "), run.Stderr)
		}
	}
}
