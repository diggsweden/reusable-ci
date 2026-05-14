// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package gpg shells out to the real `gpg` and `gpg-connect-agent`
// binaries. The pure parsing of `gpg --with-colons` output lives in
// internal/domain/gpg.
package gpg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	domain "github.com/diggsweden/reusable-ci/internal/domain/gpg"
)

// Adapter wraps the gpg / gpg-connect-agent binaries. The two paths can
// be overridden for tests via WithGPG/Path.
type Adapter struct {
	GPGBin   string // empty → "gpg"
	AgentBin string // empty → "gpg-connect-agent"
}

// New returns an Adapter using the system gpg / gpg-connect-agent.
func New() *Adapter { return &Adapter{} }

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
// included in the error on failure.
func (a *Adapter) run(ctx context.Context, bin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w\n%s", bin, strings.Join(args, " "), err, out)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// runStdin invokes a binary with args and a stdin string.
func (a *Adapter) runStdin(ctx context.Context, stdin, bin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w\n%s", bin, strings.Join(args, " "), err, out)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// ImportKey writes keyData to a mode-0600 tempfile inside t.TempDir-style
// directory and runs `gpg --import --batch --yes <file>`. The tempfile
// is deleted before return regardless of success.
func (a *Adapter) ImportKey(ctx context.Context, keyData []byte) error {
	dir, err := os.MkdirTemp("", "reusable-ci-gpg-")
	if err != nil {
		return fmt.Errorf("mkdir tempdir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	keyPath := filepath.Join(dir, "key.pgp")
	// Mode 0600: matches the bash `umask 077` + `: > $key_file` pattern.
	if err := os.WriteFile(keyPath, keyData, 0o600); err != nil {
		return fmt.Errorf("write key file: %w", err)
	}

	if _, err := a.run(ctx, a.gpg(), "--import", "--batch", "--yes", keyPath); err != nil {
		return fmt.Errorf("gpg --import: %w", err)
	}
	return nil
}

// FirstFingerprint returns the first `fpr:` line's fingerprint from a
// `gpg --batch --with-colons --list-secret-keys` invocation. Used right
// after import to discover what was just added.
func (a *Adapter) FirstFingerprint(ctx context.Context) (string, error) {
	out, err := a.run(ctx, a.gpg(), "--batch", "--with-colons", "--list-secret-keys")
	if err != nil {
		return "", fmt.Errorf("list-secret-keys: %w", err)
	}
	fpr := domain.ParseFingerprint(out)
	if fpr == "" {
		return "", errors.New("no fingerprint found after import")
	}
	return fpr, nil
}

// ListSecretKey returns the colons output for a specific fingerprint.
// Caller pipes to domain.ParseColonsOutput.
func (a *Adapter) ListSecretKey(ctx context.Context, fingerprint string) (string, error) {
	return a.run(ctx, a.gpg(), "--batch", "--with-colons", "--list-secret-keys", fingerprint)
}

// ListKeygrips returns the `gpg ... --with-keygrip` colons text for a
// specific fingerprint. Caller pipes to domain.ParseKeygrips.
func (a *Adapter) ListKeygrips(ctx context.Context, fingerprint string) (string, error) {
	return a.run(ctx, a.gpg(),
		"--batch", "--with-colons", "--with-keygrip", "--list-secret-keys", fingerprint)
}

// ConfigureAgent writes the canonical gpg-agent.conf into the resolved
// GNUPGHOME (env or $HOME/.gnupg) at mode 0600 inside a 0700 directory,
// then asks gpg-agent to reload. Idempotent.
//
// Mirrors configure_agent in scripts/release/import-gpg-key.sh.
func (a *Adapter) ConfigureAgent(ctx context.Context) error {
	home := os.Getenv("GNUPGHOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".gnupg")
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("mkdir GNUPGHOME: %w", err)
	}
	if err := os.Chmod(home, 0o700); err != nil {
		return fmt.Errorf("chmod GNUPGHOME: %w", err)
	}
	confPath := filepath.Join(home, "gpg-agent.conf")
	if err := os.WriteFile(confPath, []byte(domain.AgentConfig), 0o600); err != nil {
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
	hex := domain.HexEncodePassphrase(passphrase)
	cmd := fmt.Sprintf("PRESET_PASSPHRASE %s -1 %s\n", keygrip, hex)
	if _, err := a.runStdin(ctx, cmd, a.agent(), "/bye"); err != nil {
		return fmt.Errorf("preset passphrase: %w", err)
	}
	return nil
}

// DeleteSecretKey removes the secret half of the key. Idempotent: a
// missing fingerprint is silently swallowed (matches the bash `|| true`).
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

// DetachSign produces an ASCII-armored detached signature next to file
// (i.e. <file>.asc) using the secret key identified by keyID. Mirrors
// `ci_gpg_sign` (gpg --armor --detach-sign --default-key <id> <file>).
//
// --batch keeps gpg non-interactive (no TTY in CI). --yes makes the
// call idempotent across workflow retries: an existing <file>.asc
// from a partial earlier run is overwritten with a fresh signature
// rather than causing gpg to prompt or fail with "file exists." Same
// flag pair every other gpg call in this adapter (ImportKey, DeleteKey,
// DeleteSecretKey) already carries.
//
// Errors include the combined stderr.
func (a *Adapter) DetachSign(ctx context.Context, keyID, file string) error {
	_, err := a.run(ctx, a.gpg(),
		"--batch", "--yes", "--armor", "--detach-sign", "--default-key", keyID, file)
	return err
}
