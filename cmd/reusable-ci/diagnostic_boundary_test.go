// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	urfave "github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
)

func TestPanicTitleBoundary_PreservesUTF8AndByteBudget(t *testing.T) {
	t.Parallel()
	// These are trusted summaries, not raw payloads: formatPanic reports only
	// a fixed category. Keep the URL encoder's byte boundary independently tested.
	for _, tc := range []struct{ name, summary, title string }{
		{"119 bytes", strings.Repeat("a", 112), "panic: " + strings.Repeat("a", 112)},
		{"120 bytes", strings.Repeat("a", 113), "panic: " + strings.Repeat("a", 113)},
		{"121 bytes", strings.Repeat("a", 114), "panic: " + strings.Repeat("a", 110) + "..."},
		{"two byte", strings.Repeat("\u00e9", 80), "panic: " + strings.Repeat("\u00e9", 55) + "..."},
		{"three byte", strings.Repeat("\u20ac", 80), "panic: " + strings.Repeat("\u20ac", 36) + "..."},
		{"four byte", strings.Repeat("\U0001f600", 60), "panic: " + strings.Repeat("\U0001f600", 27) + "..."},
		{"mixed boundary", strings.Repeat("a", 109) + "\u20ac" + "zzzzz", "panic: " + strings.Repeat("a", 109) + "..."},
		{"invalid UTF8", "bad\xffbytes", "panic: bad?bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			link, err := url.Parse(bugReportLink(tc.summary, "v1", "sha", nil))
			require.NoError(t, err)

			title := link.Query().Get("title")
			require.True(t, utf8.ValidString(title))
			require.LessOrEqual(t, len(title), 120)
			require.Equal(t, tc.title, title)
		})
	}
}

func TestPanicBoundary_OwnedRootChild(t *testing.T) {
	for _, mode := range []string{"panic-string", "panic-struct", "panic-error", "panic-formatter", "panic-nil"} {
		t.Run(mode, func(t *testing.T) {
			stderr := runDiagnosticChild(t, mode, nil, 70)
			require.Contains(t, stderr, "reusable-ci: internal error:")
			require.Contains(t, stderr, "value [redacted]")
			require.Contains(t, stderr, "Stack trace (argument values omitted):")
			require.Contains(t, stderr, "main.go:")
			require.Contains(t, stderr, "diagnostic_boundary_test.go:")
			require.Contains(t, stderr, "/cmd/reusable-ci.run\n")
			require.Contains(t, stderr, "/cmd/reusable-ci.main\n")
			require.Contains(t, stderr, "Context: version=dev  commit=none  command=[redacted]")
			require.Contains(t, stderr, "please report it")
			require.NotContains(t, stderr, "goroutine ")
			require.NotContains(t, stderr, "%!")

			lines := strings.Split(strings.TrimSpace(stderr), "\n")
			link, err := url.Parse(strings.TrimSpace(lines[len(lines)-1]))
			require.NoError(t, err)
			require.Equal(t, bugReportURL, link.Scheme+"://"+link.Host+link.Path)
			require.Contains(t, link.Query().Get("title"), "value [redacted]")
			require.Contains(t, link.Query().Get("body"), "version: dev\n- commit: none\n- command: [redacted]")

			for _, secret := range []string{"fixture-argv-token", "fixture-url-password", "fixture-key-path", "fixture-env-token", "fixture-env-key", "fixture-stdin-passphrase", "fixture-unknown-payload", "formatter must not run"} {
				require.NotContains(t, stderr, secret)
				require.NotContains(t, link.Query().Get("title"), secret)
				require.NotContains(t, link.Query().Get("body"), secret)
			}
		})
	}
}

// TestDiagnosticBoundaryChild is not a stand-alone CLI binary: the test
// executable installs one inert leaf on the real root and then calls main.
// Only this child runs process hardening; no production action/deps run.
func TestDiagnosticBoundaryChild(t *testing.T) {
	mode := os.Getenv("RC_DIAGNOSTIC_CHILD")
	if mode == "" {
		return
	}

	newCommand = func(info cli.BuildInfo) *urfave.Command {
		root := cli.New(info)
		root.Commands = append(root.Commands, &urfave.Command{
			Name: "audit-boundary",
			Action: func(ctx context.Context, _ *urfave.Command) error {
				fmt.Println("ready") // run has registered both signals before entering this action.

				if !strings.HasPrefix(mode, "panic-") {
					<-ctx.Done()
					fmt.Println("cancelled")
				}

				input := bufio.NewScanner(os.Stdin)
				require.True(t, input.Scan()) // parent releases cleanup/panic, or kills this child on timeout.

				if strings.HasPrefix(mode, "panic-") {
					fmt.Println("panicking")

					payload := strings.Join(os.Args, " ") + os.Getenv("CI_TOKEN") + os.Getenv("GPG_PRIVATE_KEY") + input.Text() + "fixture-unknown-payload"

					switch mode {
					case "panic-string":
						panic(payload)
					case "panic-struct":
						panic(struct{ Secret string }{payload})
					case "panic-error":
						panic(sentinelError{msg: payload})
					case "panic-formatter":
						panic(panicFormatter{secret: payload})
					case "panic-nil":
						panic(nil)
					}
				}

				fmt.Println("cleanup complete")

				if mode == "success" {
					return nil
				}

				return ctx.Err()
			},
		})

		return root
	}
	os.Args = []string{"reusable-ci", "audit-boundary", "--", "--token=fixture-argv-token", "https://user:fixture-url-password@example.invalid", "--private-key-file=fixture-key-path"}

	main()
	t.Fatal("main returned without exiting")
}

func runDiagnosticChild(t *testing.T, mode string, sig os.Signal, wantCode int) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)

	binary, err := os.Executable()
	require.NoError(t, err)

	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestDiagnosticBoundaryChild$") //nolint:gosec // owned test executable, exact helper selection, no shell or inherited environment.
	root := t.TempDir()
	cmd.Dir = root
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + root, "TMPDIR=" + root, "RC_DIAGNOSTIC_CHILD=" + mode, "CI_TOKEN=fixture-env-token", "GPG_PRIVATE_KEY=fixture-env-key"}
	cmd.WaitDelay = time.Second

	var stderr bytes.Buffer

	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	waited := false

	t.Cleanup(func() {
		if !waited {
			_ = cmd.Process.Kill() // only the process returned by our own Start, never a group.
			_ = cmd.Wait()
		}
	})

	lines := bufio.NewScanner(stdout)
	require.True(t, lines.Scan(), "child must announce readiness")
	require.Equal(t, "ready", lines.Text())

	if sig != nil {
		require.NoError(t, cmd.Process.Signal(sig))
		require.True(t, lines.Scan(), "child must acknowledge cancellation")
		require.Equal(t, "cancelled", lines.Text())
	}

	if mode == "force" {
		require.NoError(t, cmd.Process.Signal(sig))
	} else {
		_, err = fmt.Fprintln(stdin, "fixture-stdin-passphrase")
		require.NoError(t, err)
		require.True(t, lines.Scan(), "child must reach cleanup or panic")

		want := "cleanup complete"
		if sig == nil {
			want = "panicking"
		}

		require.Equal(t, want, lines.Text())
	}
	// Drain to EOF before Wait closes StdoutPipe; a failed handshake is bounded
	// by CommandContext, and no stderr buffer is read before Wait joins its copier.
	var extra []string
	for lines.Scan() {
		extra = append(extra, lines.Text())
	}

	require.NoError(t, lines.Err())

	err = cmd.Wait()
	waited = true

	require.NoError(t, ctx.Err(), "child exceeded its deadline")

	if wantCode == 0 {
		require.NoError(t, err, "stderr: %s", stderr.String())
	} else {
		var exit *exec.ExitError
		require.ErrorAs(t, err, &exit, "stderr: %s", stderr.String())
		require.Equal(t, wantCode, exit.ExitCode(), "stderr: %s", stderr.String())
	}

	require.Empty(t, extra, "unexpected child stdout")

	return stderr.String()
}
