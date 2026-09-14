//go:build smoke

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
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

	stdout, stderr, code := runBinary(t, bin, "--help")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}

	if !strings.Contains(stdout, "artifact") {
		t.Errorf("--help missing command list; got %q", stdout)
	}

	// Explicit help is a successful request, so it belongs on stdout with
	// nothing on stderr. The distinction is not cosmetic: a caller piping
	// `--help` into a pager or a doc generator gets the text from stdout, and
	// a CI step that treats any stderr output as a warning would flag every
	// help invocation.
	if stderr != "" {
		t.Errorf("--help wrote to stderr: %q", stderr)
	}
}

// CLI-02: the `help` command prints the same root page as --help, and a group
// page lists that group's own commands, not another group's.
func TestBinary_HelpCommandAndGroupHelp(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)

	root, _, _ := runBinary(t, bin, "--help")

	stdout, stderr, code := runBinary(t, bin, "help")
	if code != 0 || stderr != "" || stdout != root {
		t.Errorf("help: exit = %d, stderr = %q, stdout equal to --help = %t", code, stderr, stdout == root)
	}

	stdout, stderr, code = runBinary(t, bin, "artifact", "--help")
	if code != 0 || stderr != "" {
		t.Fatalf("artifact --help: exit = %d, stderr = %q", code, stderr)
	}

	for _, own := range []string{"reusable-ci artifact", "digest", "download", "upload"} {
		if !strings.Contains(stdout, own) {
			t.Errorf("artifact --help does not name %q:\n%s", own, stdout)
		}
	}

	for _, foreign := range []string{"ledger", "resolve-metadata", "doctor"} {
		if strings.Contains(stdout, foreign) {
			t.Errorf("artifact --help names another group's %q:\n%s", foreign, stdout)
		}
	}
}

// CLI-04: an unknown command is a usage error (2) with a suggestion on stderr,
// and never auto-runs the guessed command.
//
// A near miss of a stable command pins the whole diagnostic. Checking for
// "unknown subcommand" alone passed with the suggestion missing, wrong, or
// followed by a usage dump. The distant name keeps the exit-code and stdout
// half: urfave suggests its closest match even then, so its text is not
// worth pinning.
func TestBinary_UnknownCommand_ExitsUsageWithSuggestion(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)

	for arg, wantStderr := range map[string]string{
		"relese":    "Error: unknown subcommand 'relese'. Did you mean \"release\"?: usage error\n",
		"bogus-cmd": "",
	} {
		stdout, stderr, code := runBinary(t, bin, arg)
		if code != 2 {
			t.Errorf("%s: exit = %d, want 2; stderr=%q", arg, code, stderr)
		}

		if wantStderr != "" && stderr != wantStderr {
			t.Errorf("%s: stderr = %q, want %q", arg, stderr, wantStderr)
		}

		if !strings.Contains(stderr, "unknown subcommand '"+arg+"'") {
			t.Errorf("%s: stderr should name the unknown subcommand; got %q", arg, stderr)
		}

		if stdout != "" {
			t.Errorf("%s: a rejected command must not produce stdout output; got %q", arg, stdout)
		}
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

	// An existing empty directory reaches ledger validation, not the arg guard.
	dir := t.TempDir()
	output := filepath.Join(t.TempDir(), "merged.json")
	_, stderr, code := runBinary(t, bin, "container", "ledger", "merge", "--ledger", output, dir)
	if code != int(errs.ExitCodeNoInput) || !strings.Contains(stderr, "no release-image entries found") {
		t.Fatalf("exit = %d, want missing-input refusal after positional acceptance; stderr=%q", code, stderr)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Errorf("refused merge created output: %v", err)
	}

	if strings.Contains(stderr, "unexpected argument") {
		t.Errorf("an ArgsUsage command must not be arg-guarded; got %q", stderr)
	}
}

// CLI-05: --flag=value and --flag value parse equivalently.
func TestBinary_FlagFormEquivalence(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)

	eqOut, eqErr, eqCode := runBinary(t, bin, "--log-level=warn", "--version")
	spOut, spErr, spCode := runBinary(t, bin, "--log-level", "warn", "--version")

	if eqCode != 0 || spCode != 0 {
		t.Fatalf("exit codes: eq=%d space=%d, want 0/0", eqCode, spCode)
	}

	if eqOut != spOut {
		t.Errorf("--flag=value and --flag value differ on stdout:\n eq=%q\n sp=%q", eqOut, spOut)
	}

	// Both streams, not just stdout. The two spellings are parsed by different
	// paths, and the way one of them goes wrong without changing stdout is a
	// deprecation notice or a parse warning on stderr -- exactly the output a
	// stdout-only comparison cannot see.
	if eqErr != spErr {
		t.Errorf("--flag=value and --flag value differ on stderr:\n eq=%q\n sp=%q", eqErr, spErr)
	}

	// And neither spelling may be noisy: equal-but-both-warning would satisfy
	// the comparison above.
	if eqErr != "" {
		t.Errorf("a valid --log-level spelling wrote to stderr: %q", eqErr)
	}
}

// CLI-09: every invalid global enum value is a usage error on stderr alone,
// naming the flag and each accepted value.
func TestBinary_InvalidGlobalEnum_ExitsUsageNamingAcceptedValues(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)

	for flag, accepted := range map[string][]string{
		"log-level": {"debug", "info", "warn", "error"},
		"format":    {"auto", "text", "json", "github", "forgejo", "gitlab"},
		"provider":  {"auto", "github", "gitlab", "forgejo", "local"},
		"runner":    {"auto", "github", "forgejo", "gitlab", "local"},
	} {
		stdout, stderr, code := runBinary(t, bin, "--"+flag+"=bogus", "doctor")
		if code != 2 || stdout != "" {
			t.Errorf("--%s=bogus: exit = %d, stdout = %q, want 2 and no stdout", flag, code, stdout)
		}

		if !strings.Contains(stderr, flag) || !strings.Contains(stderr, `"bogus"`) {
			t.Errorf("--%s=bogus: stderr does not name the flag and value: %q", flag, stderr)
		}

		for _, value := range accepted {
			if !strings.Contains(stderr, value) {
				t.Errorf("--%s=bogus: stderr does not name accepted %q: %q", flag, value, stderr)
			}
		}
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

	// Which stream carried it is the point, and the old assertion searched
	// stdout+stderr concatenated, so it could not tell. Progress narration
	// belongs on stderr: stdout is where this command's machine-readable
	// output goes, and a summary line mixed into it is parsed by whatever
	// consumes the upload result.
	const summary = "Uploaded x (0 files, 0 bytes)"
	if !strings.Contains(stderr, summary) {
		t.Errorf("stderr does not carry the no-op summary %q; got %q", summary, stderr)
	}

	if stdout != "" {
		t.Errorf("a no-op upload wrote to stdout: %q", stdout)
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

// PAR-CAP-2 offline: SARIF upload on a forge without code scanning degrades to
// one notice that names SARIF, says the upload was skipped and names the
// platform, exits 0 and writes nothing to stdout, before any forge call. The
// live degradation scenario asserts the same text against a real forge.
func TestBinary_SARIFUploadDegradesWithAnExplanatoryNotice(t *testing.T) {
	_ = testenv.New(t)
	bin := buildBinary(t)

	sarif := filepath.Join(t.TempDir(), "findings.sarif")
	if err := os.WriteFile(sarif, []byte(`{"version":"2.1.0","runs":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, forge := range []string{"gitlab", "forgejo", "local"} {
		stdout, stderr, code := runBinary(t, bin, "--provider", forge, "--format", "text",
			"security", "report", "upload-sarif", "--sarif-file", sarif, "--repository", "owner/repo")

		want := `Notice: SARIF upload skipped — SARIF upload to Code Scanning is not supported on platform "` + forge + `": unsupported on this platform` + "\n"
		if code != 0 || stdout != "" || stderr != want {
			t.Errorf("%s: exit %d, stdout %q, stderr %q, want exit 0, no stdout and %q", forge, code, stdout, stderr, want)
		}
	}
}
