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

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domaingpg "github.com/diggsweden/reusable-ci/internal/domain/gpg"
	"github.com/diggsweden/reusable-ci/internal/safeexec"
)

// Adapter wraps the gpg / gpg-connect-agent binaries. Both paths can be
// overridden for tests via the public fields.
type Adapter struct {
	GPGBin   string // empty → "gpg"
	AgentBin string // empty → "gpg-connect-agent"
}

// New returns an Adapter using the system gpg / gpg-connect-agent.
func New() *Adapter { return &Adapter{} }

// ImportKey runs `gpg --import --batch --yes` with the key piped on
// stdin. The key bytes never touch disk — they go process-to-process
// via the inherited pipe, then live only in gpg's own keyring (which
// gpg manages under GNUPGHOME with its own mode-0600 guarantee).
//
// The on-disk import target is gpg's keyring rather than ours: the
// downstream `git tag -s` / `git commit -S` calls shell to gpg
// themselves and read from that keyring.
func (a *Adapter) ImportKey(ctx context.Context, keyData []byte) error {
	if _, err := a.runStdin(ctx, string(keyData), a.gpg(), "--import", "--batch", "--yes"); err != nil {
		return fmt.Errorf("gpg --import: %w", err)
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
	if _, err := a.runStdin(ctx, cmd, a.agent(), "/bye"); err != nil {
		return fmt.Errorf("preset passphrase: %w", err)
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

	out, err := cmd.CombinedOutput()

	return finishRun(bin, args, out, err)
}

// runStdin invokes a binary with args and a stdin string.
func (a *Adapter) runStdin(ctx context.Context, stdin, bin string, args ...string) (string, error) {
	cmd := safeexec.Command(ctx, bin, args...)
	cmd.Stdin = strings.NewReader(stdin)

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
