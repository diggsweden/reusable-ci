// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package gpg integrates with the on-disk GPG keyring and gpg-agent so
// git's own signing path — `git tag -s`, `git commit -S`, `git tag -v`
// — works in the runner. Everything that can be done in-process lives
// in adapters/openpgp (signing, verification, metadata extraction);
// this adapter is reserved for operations that intrinsically require a
// real keyring + agent.
package gpg

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domaingpg "github.com/diggsweden/reusable-ci/v3/internal/domain/gpg"
	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)

// Adapter wraps the gpg / gpg-connect-agent binaries. Both paths can be
// overridden for tests via the public fields.
type Adapter struct {
	GPGBin   string // empty → "gpg"
	AgentBin string // empty → "gpg-connect-agent"
	Env      []string
}

// New returns an Adapter using the system gpg / gpg-connect-agent.
func New() *Adapter { return &Adapter{} }

// NewIsolated returns an Adapter whose subprocesses see only runtime env vars
// such as PATH/GNUPGHOME/TMPDIR, not the private key or passphrase env that the
// reusable-ci process may have read before invoking gpg.
func NewIsolated() *Adapter { return &Adapter{Env: IsolatedEnv()} }

// ImportKey runs `gpg --import --batch --yes` with the key piped on
// stdin. The key bytes never touch disk — they go process-to-process
// via the inherited pipe, then live only in gpg's own keyring (which
// gpg manages under GNUPGHOME with its own mode-0600 guarantee).
//
// The on-disk import target is gpg's keyring rather than ours: the
// downstream `git tag -s` / `git commit -S` calls shell to gpg
// themselves and read from that keyring.
func (a *Adapter) ImportKey(ctx context.Context, keyData []byte) error {
	cmd := safeexec.Command(ctx, a.gpg(), "--import", "--batch", "--yes")

	cmd.Stdin = strings.NewReader(string(keyData))
	if a.Env != nil {
		cmd.Env = a.Env
	}

	// Deliberately NOT finishRun: import stdin is key material by
	// definition, and gpg echoes input fragments in its diagnostics
	// ("invalid armor header: <line>") that carry no private-key marker,
	// so RedactKeyMaterial cannot catch them. Suppress gpg's output
	// wholesale; the exit classification is diagnostic enough.
	if _, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gpg --import (diagnostics suppressed; import input is key material): %w",
			safeexec.WrapError(err, a.gpg(), "--import"))
	}

	return nil
}

// ListKeygrips returns `gpg --with-keygrip --with-colons --list-secret-keys`
// output for a specific fingerprint. The caller pipes it to
// domaingpg.ParseKeygrips. Keygrips are the address gpg-agent uses for
// passphrase pre-seeding (one per signing-capable subkey).
func (a *Adapter) ListKeygrips(ctx context.Context, fingerprint string) (string, error) {
	return a.run(ctx, a.gpg(),
		"--batch", "--with-colons", "--with-keygrip", "--list-secret-keys", fingerprint)
}

// ListSecretKeys returns machine-readable secret-key metadata for the current
// keyring. Callers parse fpr records to verify the imported key fingerprint.
func (a *Adapter) ListSecretKeys(ctx context.Context) (string, error) {
	return a.run(ctx, a.gpg(), "--batch", "--with-colons", "--list-secret-keys")
}

// DetachedSign creates a binary detached signature at outputPath using loopback
// pinentry. The passphrase is provided on stdin, never argv or environment.
func (a *Adapter) DetachedSign(ctx context.Context, fingerprint, passphrase, inputPath, outputPath string) error {
	_, err := a.runStdin(ctx, passphrase, a.gpg(),
		"--batch",
		"--pinentry-mode", "loopback",
		"--passphrase-fd", "0",
		"--local-user", fingerprint,
		"--output", outputPath,
		"--detach-sign", inputPath)
	if err != nil {
		return fmt.Errorf("gpg --detach-sign %s: %w", inputPath, err)
	}

	return nil
}

// ConfigureAgent writes the canonical gpg-agent.conf into the resolved
// GNUPGHOME (env or $HOME/.gnupg) at mode 0600 inside a 0700 directory,
// then asks gpg-agent to reload. Idempotent.
func (a *Adapter) ConfigureAgent(ctx context.Context) error {
	home := os.Getenv("GNUPGHOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".gnupg")
		// Writing into the user's real default keyring home rather than an
		// isolated GNUPGHOME. Say so (clig.dev §Configuration: tell the
		// user when you touch config that isn't yours) — CI runners set
		// GNUPGHOME; a developer who didn't may not expect ~/.gnupg to be
		// modified. The written file carries a managed-by marker too.
		slog.Warn("GNUPGHOME unset; writing gpg-agent.conf into the default keyring home",
			"path", home, "hint", "set GNUPGHOME to isolate reusable-ci's gpg state")
	}

	if err := os.MkdirAll(home, 0o700); err != nil { //nolint:gosec // GNUPGHOME path comes from env, not external input.
		return fmt.Errorf("mkdir GNUPGHOME: %w", err)
	}

	if err := os.Chmod(home, 0o700); err != nil { //nolint:gosec // GNUPGHOME path comes from env, not external input.
		return fmt.Errorf("chmod GNUPGHOME: %w", err)
	}

	confPath := filepath.Join(home, "gpg-agent.conf")
	if err := os.WriteFile(confPath, []byte(domaingpg.AgentConfig), 0o600); err != nil { //nolint:gosec // GNUPGHOME path is env-controlled.
		return fmt.Errorf("write gpg-agent.conf: %w", err)
	}

	if _, err := a.run(ctx, a.agent(), "reloadagent", "/bye"); err != nil {
		return fmt.Errorf("reload gpg-agent: %w", err)
	}

	return nil
}

// PresetPassphrase caches `passphrase` against `keygrip` via gpg-agent.
// The hex-encoded passphrase is fed through stdin so it never appears in
// `ps`. Improvement over the upstream action's argv-based form.
func (a *Adapter) PresetPassphrase(ctx context.Context, keygrip, passphrase string) error {
	hex := domaingpg.HexEncodePassphrase(passphrase)

	cmd := fmt.Sprintf("PRESET_PASSPHRASE %s -1 %s\n", keygrip, hex)

	transcript, err := a.runStdin(ctx, cmd, a.agent(), "/bye")
	if err != nil {
		return fmt.Errorf("preset passphrase: %w", err)
	}

	// The exit status above proves nothing — gpg-connect-agent exits 0 for
	// an "ERR" answer and for "no agent running" alike. The transcript is
	// the only evidence the agent actually took the passphrase, and if it
	// did not we must fail HERE: the alternative is gpg reaching for
	// pinentry during `git tag -s` several steps later, in a container
	// with no TTY, and reporting an ioctl error.
	if ok, detail := domaingpg.AgentAck(transcript); !ok {
		return fmt.Errorf(
			"preset passphrase for keygrip %s: gpg-agent did not acknowledge: %s: %w",
			keygrip, safeexec.RedactKeyMaterial([]byte(detail)), errs.ErrDependencyUnavailable)
	}

	return nil
}

// ExportSecretKey returns the ASCII-armored secret key for fingerprint.
//
// This exists for Gradle signing. Gradle's useInMemoryPgpKeys goes
// through Bouncycastle, which reads only RFC 4880 packets — keys
// exported by newer GnuPG in the v5 format are rejected — so the key has
// to be re-exported from the keyring rather than passed through as the
// raw RELEASE_GPG_PRIVATE_KEY secret.
//
// Unlike run / runStdin this does NOT use CombinedOutput, and the empty
// check below is not belt-and-braces. Both exist because of one verified
// gpg behaviour: **exporting an absent key exits 0**, writing nothing to
// stdout and only "gpg: WARNING: nothing exported" to stderr. Under
// CombinedOutput that warning is returned as if it were the key, and the
// caller signs nothing while believing it holds key material. Capturing
// stdout separately and rejecting an empty result is what turns that
// silent success into an error.
//
// Stderr is kept separate and only surfaces (redacted) on failure.
//
// The returned string is private key material. Callers must pass it
// straight into a child-process environment — never to a sink, a step
// output, stdout, or a file.
func (a *Adapter) ExportSecretKey(ctx context.Context, fingerprint, passphrase string) (string, error) {
	args := []string{
		"--batch", "--yes",
		"--pinentry-mode", "loopback",
		"--passphrase-fd", "0",
		"--armor",
		"--export-secret-keys", fingerprint,
	}

	cmd := safeexec.Command(ctx, a.gpg(), args...)
	cmd.Stdin = strings.NewReader(passphrase)

	// Same isolation contract as every other method here: an Adapter built
	// by NewIsolated must not leak the private-key/passphrase environment
	// into gpg. Omitting this is exactly the leak NewIsolated exists to
	// prevent, and this method handles key material.
	if a.Env != nil {
		cmd.Env = a.Env
	}

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		wrapped := safeexec.WrapError(err, a.gpg(), "--export-secret-keys")
		if stderr.Len() == 0 {
			return "", wrapped
		}

		return "", fmt.Errorf("%w\n%s", wrapped, safeexec.RedactKeyMaterial(stderr.Bytes()))
	}

	key := stdout.String()
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("gpg exported an empty secret key for %q: %w", fingerprint, errs.ErrValidation)
	}

	return key, nil
}

// DeleteSecretKey removes the secret half of the key. Idempotent:
// errors are swallowed so it can run unconditionally in cleanup paths.
func (a *Adapter) DeleteSecretKey(ctx context.Context, fingerprint string) {
	_, _ = a.run(ctx, a.gpg(), "--batch", "--yes", "--delete-secret-keys", fingerprint)
}

// DeleteKey removes the public half. Idempotent: errors swallowed.
func (a *Adapter) DeleteKey(ctx context.Context, fingerprint string) {
	_, _ = a.run(ctx, a.gpg(), "--batch", "--yes", "--delete-keys", fingerprint)
}

// KillAgent stops gpg-agent. Idempotent: errors swallowed.
func (a *Adapter) KillAgent(ctx context.Context) {
	_, _ = a.run(ctx, a.agent(), "KILLAGENT", "/bye")
}

func (a *Adapter) gpg() string {
	if a.GPGBin != "" {
		return a.GPGBin
	}

	return "gpg"
}

func (a *Adapter) agent() string {
	if a.AgentBin != "" {
		return a.AgentBin
	}

	return "gpg-connect-agent"
}

// run invokes a binary with args and combined output captured. Stdout
// is returned trimmed of a single trailing newline; combined output is
// appended after the classified error so operators still see the tool's
// own diagnostic text in CI logs.
func (a *Adapter) run(ctx context.Context, bin string, args ...string) (string, error) {
	cmd := safeexec.Command(ctx, bin, args...)
	if a.Env != nil {
		cmd.Env = a.Env
	}

	out, err := cmd.CombinedOutput()

	return finishRun(bin, args, out, err)
}

// runStdin invokes a binary with args and a stdin string.
func (a *Adapter) runStdin(ctx context.Context, stdin, bin string, args ...string) (string, error) {
	cmd := safeexec.Command(ctx, bin, args...)

	cmd.Stdin = strings.NewReader(stdin)
	if a.Env != nil {
		cmd.Env = a.Env
	}

	out, err := cmd.CombinedOutput()

	return finishRun(bin, args, out, err)
}

// finishRun is the shared post-processor for run / runStdin: turn the
// (out, err) pair into the (trimmed stdout, classified error) pair.
//
// On error, the captured combined-output is appended to the wrapped
// error so operators see gpg's own diagnostic text in CI logs — UNLESS
// it contains a private-key marker, in which case the body is replaced
// wholesale (RedactKeyMaterial). Defends against a future gpg version
// echoing input key material on stderr.
func finishRun(bin string, args []string, out []byte, err error) (string, error) {
	if err == nil {
		return strings.TrimRight(string(out), "\n"), nil
	}

	wrapped := safeexec.WrapError(err, bin, safeexec.FirstArg(args))
	if len(out) == 0 {
		return "", wrapped
	}

	return "", fmt.Errorf("%w\n%s", wrapped, safeexec.RedactKeyMaterial(out))
}
