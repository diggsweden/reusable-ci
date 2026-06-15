//go:build e2e

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/testutil/testenv"
)

// These tests implement the global-contract and artifact-CLI scenarios from
// docs/cli-black-box.md. They assert the deterministic, off-runner surface
// only: exit codes, stream discipline, and the argument/collection validation
// that resolves before any forge transport. The artifact "results" upload
// backend itself is untestable off-runner (see ART-* in the doc).

// CLI-01: no args prints help to stdout, exits 0, leaves stderr clean.
func TestBinary_NoArgs_PrintsHelpOnStdout(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)

	stdout, stderr, code := runBinary(t, bin)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr)
	}

	if !strings.Contains(stdout, "artifact") || !strings.Contains(stdout, "COMMANDS") {
		t.Errorf("help should list command groups on stdout; got %q", stdout)
	}

	if strings.TrimSpace(stderr) != "" {
		t.Errorf("stderr should be empty for help; got %q", stderr)
	}
}

// CLI-02: --help exits 0 with the command surface on stdout.
func TestBinary_Help_ExitsZero(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)

	stdout, _, code := runBinary(t, bin, "--help")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}

	if !strings.Contains(stdout, "artifact") {
		t.Errorf("--help missing command list; got %q", stdout)
	}
}

// CLI-04: an unknown command is a usage error (2) with a suggestion on stderr,
// and never auto-runs the guessed command.
func TestBinary_UnknownCommand_ExitsUsageWithSuggestion(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)

	stdout, stderr, code := runBinary(t, bin, "bogus-cmd")
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr=%q", code, stderr)
	}

	if !strings.Contains(stderr, "unknown subcommand") {
		t.Errorf("stderr should name the unknown subcommand; got %q", stderr)
	}

	if strings.TrimSpace(stdout) != "" {
		t.Errorf("a rejected command must not produce stdout output; got %q", stdout)
	}
}

// CLI-06: a leaf command rejects an unexpected positional argument with a
// usage error (exit 2) instead of silently ignoring it (F1).
func TestBinary_LeafRejectsUnexpectedPositional(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)

	_, stderr, code := runBinary(t, bin, "doctor", "unexpected-positional")
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr=%q", code, stderr)
	}

	if !strings.Contains(stderr, "unexpected argument") {
		t.Errorf("stderr should name the unexpected argument; got %q", stderr)
	}
}

// CLI-06b: a command that declares ArgsUsage (it genuinely takes positionals)
// is NOT arg-guarded — `container ledger merge <path>` accepts its path.
func TestBinary_ArgsUsageCommandAcceptsPositionals(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)

	// A non-existent ledger dir is a graceful no-op, not an arg rejection.
	dir := filepath.Join(t.TempDir(), "absent")
	_, stderr, code := runBinary(t, bin, "container", "ledger", "merge", dir)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (positional accepted); stderr=%q", code, stderr)
	}

	if strings.Contains(stderr, "unexpected argument") {
		t.Errorf("an ArgsUsage command must not be arg-guarded; got %q", stderr)
	}
}

// CLI-05: --flag=value and --flag value parse equivalently.
func TestBinary_FlagFormEquivalence(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)

	eqOut, _, eqCode := runBinary(t, bin, "--log-level=warn", "--version")
	spOut, _, spCode := runBinary(t, bin, "--log-level", "warn", "--version")

	if eqCode != 0 || spCode != 0 {
		t.Fatalf("exit codes: eq=%d space=%d, want 0/0", eqCode, spCode)
	}

	if eqOut != spOut {
		t.Errorf("--flag=value and --flag value differ:\n eq=%q\n sp=%q", eqOut, spOut)
	}
}

// CLI-09: an invalid global enum value is a usage error naming the field.
func TestBinary_InvalidFormat_ExitsUsage(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)

	_, stderr, code := runBinary(t, bin, "--format=bogus", "doctor")
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr=%q", code, stderr)
	}

	if !strings.Contains(strings.ToLower(stderr), "format") {
		t.Errorf("stderr should name the bad format flag; got %q", stderr)
	}
}

// ART-01: artifact upload with no selector is a usage error.
func TestBinary_ArtifactUpload_NoSelectorExitsUsage(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)

	_, stderr, code := runBinary(t, bin, "--provider", "github", "artifact", "upload", "--name", "x")
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr=%q", code, stderr)
	}

	if !strings.Contains(stderr, "needs --dir, --path, or at least one --file") {
		t.Errorf("stderr should explain the missing selector; got %q", stderr)
	}
}

// ART-02: artifact download with both --name and --pattern is a usage error.
func TestBinary_ArtifactDownload_NameAndPatternConflictExitsUsage(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)

	dir := t.TempDir()
	_, stderr, code := runBinary(t, bin, "--provider", "github", "artifact", "download",
		"--name", "a", "--pattern", "b*", "--dir", dir)
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr=%q", code, stderr)
	}

	if !strings.Contains(stderr, "use --name or --pattern, not both") {
		t.Errorf("stderr should explain the name/pattern conflict; got %q", stderr)
	}
}

// ART-03: artifact download with neither --name nor --pattern is a usage
// error (exit 2), symmetric with upload's missing-selector (F3).
func TestBinary_ArtifactDownload_NoSelectorExitsUsage(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)

	dir := t.TempDir()
	_, stderr, code := runBinary(t, bin, "--provider", "github", "artifact", "download", "--dir", dir)
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr=%q", code, stderr)
	}

	if !strings.Contains(stderr, "download needs --name or --pattern") {
		t.Errorf("stderr should explain the missing selector; got %q", stderr)
	}
}

// ART-04: an empty glob with --if-no-files ignore is a clean no-op (exit 0),
// resolved before any transport — so it needs no runner credentials.
func TestBinary_ArtifactUpload_EmptyGlobIgnoreIsNoOp(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)

	empty := filepath.Join(t.TempDir(), "none", "*.json")
	stdout, stderr, code := runBinary(t, bin, "--provider", "github", "artifact", "upload",
		"--name", "x", "--path", empty, "--if-no-files", "ignore")
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr)
	}

	if !strings.Contains(stdout+stderr, "(0 files, 0 bytes)") {
		t.Errorf("expected a 0-files no-op summary; stdout=%q stderr=%q", stdout, stderr)
	}
}

// ART-05: an empty glob under the default --if-no-files error fails validation.
func TestBinary_ArtifactUpload_EmptyGlobDefaultErrorsValidation(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)

	empty := filepath.Join(t.TempDir(), "none", "*.json")
	_, stderr, code := runBinary(t, bin, "--provider", "github", "artifact", "upload",
		"--name", "x", "--path", empty)
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr=%q", code, stderr)
	}

	if !strings.Contains(stderr, `no files matched for artifact "x"`) {
		t.Errorf("stderr should report no files matched; got %q", stderr)
	}
}
