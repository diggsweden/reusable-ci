// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package release wires release-flow use cases.
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
// Narrow interface so app-layer tests can fake gpg without shelling to
// a real binary.
//
// The functions here are reserved for operations that intrinsically
// require the on-disk keyring + gpg-agent — that's how `git tag -s`
// and `git commit -S` discover the key and obtain the passphrase.
// Metadata extraction is supplied as a pure parser (ReadMetadataFunc)
// so the app layer doesn't have to import an adapter package.
type gpgImporter interface {
	ImportKey(ctx context.Context, keyData []byte) error
	ConfigureAgent(ctx context.Context) error
	ListKeygrips(ctx context.Context, fingerprint string) (string, error)
	PresetPassphrase(ctx context.Context, keygrip, passphrase string) error
}

// ReadMetadataFunc parses the primary-key fingerprint, key ID, and the
// first identity's name + email from armored GPG key material. Production
// callers pass adapters/openpgp.ReadMetadata; tests pass a fake.
type ReadMetadataFunc func(armor []byte) (domaingpg.Metadata, error)

// gitSigningOps is the subset of adapter/git.Repo that
// configureGitSigning needs. Run is here because the --global config
// path can't be expressed through repo.Config (which writes only the
// repo-local file).
type gitSigningOps interface {
	Run(ctx context.Context, args ...string) (string, error)
	Config(ctx context.Context, key, value string) error
}

// GPGImportInput drives the GPG import flow.
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
// Cleanup hooks are NOT registered here — callers pair this with
// GPGCleanup in an `if: always()` workflow step.
func GPGImport(
	ctx context.Context,
	gpg gpgImporter,
	readMetadata ReadMetadataFunc,
	gitRepo gitSigningOps,
	sink ci.OutputSink,
	in GPGImportInput,
	out io.Writer,
) (*domaingpg.Metadata, error) {
	if err := validateGPGImportInputs(gpg, readMetadata, in); err != nil {
		return nil, err
	}

	md, err := importKeyAndReadMetadata(ctx, gpg, readMetadata, in.PrivateKey)
	if err != nil {
		return nil, err
	}

	emitKeyMetadata(out, md)

	if err := emitMetadataToSink(ctx, sink, md); err != nil {
		return nil, err
	}

	if in.Passphrase != "" {
		if err := presetPassphraseInAgent(ctx, gpg, md.Fingerprint, in.Passphrase); err != nil {
			return nil, err
		}
	}

	if in.GitUserSigningKey && gitRepo != nil {
		if err := configureGitSigning(ctx, gitRepo, md, in); err != nil {
			return nil, fmt.Errorf("configure git signing: %w", err)
		}
	}

	return &md, nil
}

func validateGPGImportInputs(gpg gpgImporter, readMetadata ReadMetadataFunc, in GPGImportInput) error {
	if gpg == nil {
		return fmt.Errorf("gpg-import: gpg importer is required: %w", errs.ErrUsage)
	}

	if readMetadata == nil {
		return fmt.Errorf("gpg-import: metadata reader is required: %w", errs.ErrUsage)
	}

	if in.PrivateKey == "" {
		return fmt.Errorf("gpg-import: PrivateKey is required: %w", errs.ErrUsage)
	}

	return nil
}

// importKeyAndReadMetadata decodes the armored/base64 input, imports it
// into the local keyring, and returns its parsed metadata. Metadata is
// read in-process from the same key bytes we just imported (avoids a
// second `gpg --list-secret-keys` + colons-parse).
func importKeyAndReadMetadata(ctx context.Context, gpg gpgImporter, readMetadata ReadMetadataFunc, privateKey string) (domaingpg.Metadata, error) {
	keyData, err := domaingpg.DecodeKey(privateKey)
	if err != nil {
		return domaingpg.Metadata{}, fmt.Errorf("gpg-import: %w", err)
	}

	if importErr := gpg.ImportKey(ctx, keyData); importErr != nil {
		return domaingpg.Metadata{}, fmt.Errorf("gpg-import: %w", importErr)
	}

	md, err := readMetadata(keyData)
	if err != nil {
		return domaingpg.Metadata{}, fmt.Errorf("gpg-import: read metadata: %w", err)
	}

	return md, nil
}

func emitKeyMetadata(out io.Writer, md domaingpg.Metadata) {
	_, _ = fmt.Fprintln(out, "✓ Imported GPG key into the local keyring")
	_, _ = fmt.Fprintf(out, "  Fingerprint : %s\n", md.Fingerprint)
	_, _ = fmt.Fprintf(out, "  KeyID       : %s\n", md.KeyID)
	_, _ = fmt.Fprintf(out, "  Name        : %s\n", md.Name)
	_, _ = fmt.Fprintf(out, "  Email       : %s\n", md.Email)
}

func emitMetadataToSink(ctx context.Context, sink ci.OutputSink, md domaingpg.Metadata) error {
	if sink == nil {
		return nil
	}

	fields := []struct{ key, value string }{
		{"fingerprint", md.Fingerprint},
		{"keyid", md.KeyID},
		{"name", md.Name},
		{"email", md.Email},
	}
	for _, f := range fields { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if err := sink.Set(ctx, f.key, f.value); err != nil {
			return fmt.Errorf("emit %s: %w", f.key, err)
		}
	}

	return nil
}

func presetPassphraseInAgent(ctx context.Context, gpg gpgImporter, fingerprint, passphrase string) error {
	if err := gpg.ConfigureAgent(ctx); err != nil {
		return fmt.Errorf("configure gpg-agent: %w", err)
	}

	gripsText, err := gpg.ListKeygrips(ctx, fingerprint)
	if err != nil {
		return fmt.Errorf("list keygrips: %w", err)
	}

	for _, grip := range domaingpg.ParseKeygrips(gripsText) {
		if err := gpg.PresetPassphrase(ctx, grip, passphrase); err != nil {
			return fmt.Errorf("preset passphrase for %s: %w", grip, err)
		}
	}

	return nil
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
