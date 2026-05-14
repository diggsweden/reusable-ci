// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package release wires release-flow use cases. This file ports
// scripts/release/import-gpg-key.sh.
package release

import (
	"context"
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domaingpg "github.com/diggsweden/reusable-ci/internal/domain/gpg"
)

// gpgImporter is the slice of adapter/gpg.Adapter that GPGImport uses.
// Same role as gpgSigner / gpgCleaner — narrow interface so app-layer
// tests can fake gpg without shelling to a real binary.
type gpgImporter interface {
	ImportKey(ctx context.Context, keyData []byte) error
	FirstFingerprint(ctx context.Context) (string, error)
	ListSecretKey(ctx context.Context, fingerprint string) (string, error)
	ConfigureAgent(ctx context.Context) error
	ListKeygrips(ctx context.Context, fingerprint string) (string, error)
	PresetPassphrase(ctx context.Context, keygrip, passphrase string) error
}

// gitSigningOps is the subset of adapter/git.Repo that
// configureGitSigning needs. Run is here because the --global config
// path can't be expressed through repo.Config (which writes only the
// repo-local file).
type gitSigningOps interface {
	Run(ctx context.Context, args ...string) (string, error)
	Config(ctx context.Context, key, value string) error
}

// GPGImportInput drives the GPG import flow. The flag-style booleans
// mirror the GIT_USER_SIGNINGKEY / GIT_COMMIT_GPGSIGN / GIT_CONFIG_GLOBAL
// env vars from the bash, lifted to typed values.
type GPGImportInput struct {
	PrivateKey        string // armored or base64-encoded armored key (required)
	Passphrase        string // optional; configures gpg-agent + presets when set
	GitUserSigningKey bool   // write user.signingkey/name/email to git config
	GitCommitGPGSign  bool   // also write commit.gpgsign=true
	GitConfigGlobal   bool   // use --global on the git config writes
}

// GPGImport imports the key, optionally caches the passphrase, optionally
// configures git signing, and emits fingerprint/keyid/name/email through
// the OutputSink.
//
// Mirrors scripts/release/import-gpg-key.sh exactly. Non-fatal cleanup
// hooks are NOT registered here — callers pair this with GPGCleanup
// in an `if: always()` workflow step (see scripts/release/cleanup-gpg-key.sh).
func GPGImport(
	ctx context.Context,
	gpg gpgImporter,
	gitRepo gitSigningOps,
	sink ci.OutputSink,
	in GPGImportInput,
	out io.Writer,
) (*domaingpg.Metadata, error) {
	if gpg == nil {
		return nil, fmt.Errorf("gpg-import: gpg importer is required: %w", errs.ErrUsage)
	}
	if in.PrivateKey == "" {
		return nil, fmt.Errorf("gpg-import: PrivateKey is required: %w", errs.ErrUsage)
	}

	keyData, err := domaingpg.DecodeKey(in.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("gpg-import: %w", err)
	}

	if err := gpg.ImportKey(ctx, keyData); err != nil {
		return nil, fmt.Errorf("gpg-import: %w", err)
	}

	fingerprint, err := gpg.FirstFingerprint(ctx)
	if err != nil {
		return nil, fmt.Errorf("gpg-import: %w", err)
	}

	colons, err := gpg.ListSecretKey(ctx, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("gpg-import: list-secret-keys: %w", err)
	}
	md := domaingpg.ParseColonsOutput(colons)

	fmt.Fprintf(out, "Fingerprint : %s\n", md.Fingerprint)
	fmt.Fprintf(out, "KeyID       : %s\n", md.KeyID)
	fmt.Fprintf(out, "Name        : %s\n", md.Name)
	fmt.Fprintf(out, "Email       : %s\n", md.Email)

	if sink != nil {
		if err := sink.Set(ctx, "fingerprint", md.Fingerprint); err != nil {
			return nil, fmt.Errorf("emit fingerprint: %w", err)
		}
		if err := sink.Set(ctx, "keyid", md.KeyID); err != nil {
			return nil, fmt.Errorf("emit keyid: %w", err)
		}
		if err := sink.Set(ctx, "name", md.Name); err != nil {
			return nil, fmt.Errorf("emit name: %w", err)
		}
		if err := sink.Set(ctx, "email", md.Email); err != nil {
			return nil, fmt.Errorf("emit email: %w", err)
		}
	}

	if in.Passphrase != "" {
		if err := gpg.ConfigureAgent(ctx); err != nil {
			return nil, fmt.Errorf("configure gpg-agent: %w", err)
		}
		gripsText, err := gpg.ListKeygrips(ctx, fingerprint)
		if err != nil {
			return nil, fmt.Errorf("list keygrips: %w", err)
		}
		for _, grip := range domaingpg.ParseKeygrips(gripsText) {
			if err := gpg.PresetPassphrase(ctx, grip, in.Passphrase); err != nil {
				return nil, fmt.Errorf("preset passphrase for %s: %w", grip, err)
			}
		}
	}

	if in.GitUserSigningKey && gitRepo != nil {
		if err := configureGitSigning(ctx, gitRepo, md, in); err != nil {
			return nil, fmt.Errorf("configure git signing: %w", err)
		}
	}

	return &md, nil
}

func configureGitSigning(ctx context.Context, repo gitSigningOps, md domaingpg.Metadata, in GPGImportInput) error {
	cfg := func(key, value string) error {
		if in.GitConfigGlobal {
			_, err := repo.Run(ctx, "config", "--global", key, value)
			return err
		}
		return repo.Config(ctx, key, value)
	}
	if err := cfg("user.signingkey", md.KeyID); err != nil {
		return err
	}
	if err := cfg("user.name", md.Name); err != nil {
		return err
	}
	if err := cfg("user.email", md.Email); err != nil {
		return err
	}
	if in.GitCommitGPGSign {
		if err := cfg("commit.gpgsign", "true"); err != nil {
			return err
		}
	}
	return nil
}
