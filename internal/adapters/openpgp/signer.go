// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package openpgp provides in-process OpenPGP signing using
// github.com/ProtonMail/go-crypto. Replaces shell-outs to `gpg
// --detach-sign` so release-artifact signing never touches the
// filesystem GNUPGHOME, never spawns a subprocess, and never caches
// the passphrase in a long-lived gpg-agent.
//
// The disk-resident GPG flow (internal/adapters/gpg) is retained
// because git's own signing — `git tag -v`, `git commit -S` — drives
// the gpg CLI directly and needs the disk keyring + agent.
package openpgp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domaingpg "github.com/diggsweden/reusable-ci/internal/domain/gpg"
)

// Signer signs files in-process using a decrypted OpenPGP entity. Once
// constructed, the same Signer is used for every artifact in a release:
// the key material lives in memory for the lifetime of the process and
// is wiped (via Go's GC) when the Signer is dropped.
//
// Construct one via NewSignerFromArmor (production: env-sourced armored
// key) or NewSignerFromEntity (tests: in-process openpgp.NewEntity).
type Signer struct {
	entity *openpgp.Entity
}

// NewSignerFromArmor parses an ASCII-armored OpenPGP private key, applies
// the passphrase, and returns a Signer that will sign with the entity's
// primary or signing subkey.
//
// Empty armor → ErrMissingInput.
// Wrong passphrase → ErrPermissionDenied.
// Multiple entities in the armor → the first is used (matches gpg's
// `--default-key` precedence when only one fingerprint is configured).
//nolint:cyclop // keyring parse + key-type + passphrase + identity dispatch.
func NewSignerFromArmor(armor []byte, passphrase string) (*Signer, error) {
	armor = bytes.TrimSpace(armor)
	if len(armor) == 0 {
		return nil, fmt.Errorf("private key armor is empty: %w", errs.ErrMissingInput)
	}

	list, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(armor))
	if err != nil {
		return nil, fmt.Errorf("parse armored private key: %w: %w", err, errs.ErrMalformedInput)
	}

	if len(list) == 0 {
		return nil, fmt.Errorf("no entity found in armored key: %w", errs.ErrMalformedInput)
	}

	entity := list[0]
	if entity.PrivateKey == nil {
		return nil, fmt.Errorf("armor contains only a public key — need the private half to sign: %w", errs.ErrMissingInput)
	}

	if entity.PrivateKey.Encrypted {
		if passphrase == "" {
			return nil, fmt.Errorf("private key is encrypted but no passphrase was provided: %w", errs.ErrPermissionDenied)
		}

		if err := entity.PrivateKey.Decrypt([]byte(passphrase)); err != nil {
			return nil, fmt.Errorf("decrypt private key: %w: %w", err, errs.ErrPermissionDenied)
		}
	}
	// Decrypt every signing subkey too — go-crypto picks the most
	// appropriate signing key automatically, but it must be unlocked.
	for i := range entity.Subkeys {
		sub := &entity.Subkeys[i]
		if sub.PrivateKey == nil || !sub.PrivateKey.Encrypted {
			continue
		}

		if passphrase == "" {
			continue
		}

		if err := sub.PrivateKey.Decrypt([]byte(passphrase)); err != nil {
			return nil, fmt.Errorf("decrypt signing subkey: %w: %w", err, errs.ErrPermissionDenied)
		}
	}

	return &Signer{entity: entity}, nil
}

// NewSignerFromEntity wraps an already-decrypted entity. Test-only path
// in production callers; integration code uses NewSignerFromArmor.
func NewSignerFromEntity(entity *openpgp.Entity) *Signer {
	return &Signer{entity: entity}
}

// ReadMetadata parses armor (private or public key) and returns the
// primary-key fingerprint, long key ID, and the first identity's
// Name + Email. Replaces shell-outs to `gpg --list-secret-keys
// --with-colons` and the colons-format parser.
//
// The armor may carry either a private or a public block — only the
// public-key material is needed for metadata, so callers that have
// just the public side can use this too.
func ReadMetadata(armor []byte) (domaingpg.Metadata, error) {
	armor = bytes.TrimSpace(armor)
	if len(armor) == 0 {
		return domaingpg.Metadata{}, fmt.Errorf("armor is empty: %w", errs.ErrMissingInput)
	}

	list, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(armor))
	if err != nil {
		return domaingpg.Metadata{}, fmt.Errorf("parse armored key: %w: %w", err, errs.ErrMalformedInput)
	}

	if len(list) == 0 {
		return domaingpg.Metadata{}, fmt.Errorf("no entity in armor: %w", errs.ErrMalformedInput)
	}

	return entityMetadata(list[0]), nil
}

// entityMetadata extracts the typed metadata from an openpgp.Entity.
// Splits the primary identity's User ID (RFC 4880 §5.11 — "Name
// (comment) <email>") into Name + Email.
func entityMetadata(entity *openpgp.Entity) domaingpg.Metadata {
	md := domaingpg.Metadata{
		Fingerprint: strings.ToUpper(fmt.Sprintf("%X", entity.PrimaryKey.Fingerprint)),
	}
	if len(md.Fingerprint) >= 16 {
		md.KeyID = md.Fingerprint[len(md.Fingerprint)-16:]
	}

	for _, ident := range entity.Identities {
		md.Name = ident.UserId.Name
		md.Email = ident.UserId.Email

		break // first identity wins (matches `gpg --list-keys` precedence)
	}

	return md
}

// Fingerprint returns the primary-key fingerprint as a 40-char uppercase
// hex string. Matches the format `gpg --list-keys --with-colons`'s
// `fpr:::::::::<HEX>:` field produces.
func (s *Signer) Fingerprint() string {
	if s == nil || s.entity == nil || s.entity.PrimaryKey == nil {
		return ""
	}

	return strings.ToUpper(fmt.Sprintf("%X", s.entity.PrimaryKey.Fingerprint))
}

// SignFile reads path, produces an ASCII-armored detached signature, and
// writes it to <path>.asc. The signature file is written at mode 0644:
// it is not secret-bearing, downstream consumers must be able to read it.
//
// ctx is reserved for cancellation symmetry with adapter interfaces; the
// signing operation itself is CPU-bound and quick (kilobytes per
// millisecond) so context cancellation is honoured between files only.
func (s *Signer) SignFile(_ context.Context, path string) error {
	if s == nil || s.entity == nil {
		return fmt.Errorf("signer not initialised: %w", errs.ErrUsage)
	}

	in, err := os.Open(path) //nolint:gosec // caller-controlled artifact path
	if err != nil {
		return fmt.Errorf("open %q: %w", path, err)
	}

	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(path+".asc", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644) //nolint:gosec // sig file lives next to artefact; both are caller paths.
	if err != nil {
		return fmt.Errorf("create %q: %w", path+".asc", err)
	}

	defer func() { _ = out.Close() }()

	if err := openpgp.ArmoredDetachSign(out, s.entity, in, nil); err != nil {
		return fmt.Errorf("detach-sign %q: %w", path, err)
	}

	return nil
}

// Extensions returns the sidecar extensions this Signer produces.
// GPG produces exactly one detached-signature file with the `.asc`
// suffix. Returned as a slice for symmetry with the cosign Signer
// (which produces multiple sidecars in keyless mode).
func (s *Signer) Extensions() []string { return []string{".asc"} }

// VerifyDetachedArmored verifies an armored detached signature
// against an artefact, using the supplied armored public key(s) as
// the trust anchor. Wraps go-crypto's CheckArmoredDetachedSignature
// so the validate flow doesn't need to import openpgp directly.
//
// Returns nil when the signature verifies against any entity in
// pubKeyArmor. Any failure (parse, verify, IO) is wrapped with
// errs.ErrPermissionDenied so callers can detect verification
// failures distinct from infrastructure errors.
func VerifyDetachedArmored(artefact io.Reader, signature io.Reader, pubKeyArmor []byte) error {
	keyring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(pubKeyArmor))
	if err != nil {
		return fmt.Errorf("parse armored public key: %w: %w", err, errs.ErrMalformedInput)
	}

	if len(keyring) == 0 {
		return fmt.Errorf("public key armor is empty: %w", errs.ErrMissingInput)
	}

	if _, err := openpgp.CheckArmoredDetachedSignature(keyring, artefact, signature, nil); err != nil {
		return fmt.Errorf("verify detached signature: %w: %w", err, errs.ErrPermissionDenied)
	}

	return nil
}

// WriteSignature signs message and writes the armored detached signature
// to w. Used by callers that already have the message in memory and want
// to control where the signature lands (e.g. signing the checksums file
// while streaming to a buffer).
func (s *Signer) WriteSignature(w io.Writer, message io.Reader) error {
	if s == nil || s.entity == nil {
		return fmt.Errorf("signer not initialised: %w", errs.ErrUsage)
	}

	return openpgp.ArmoredDetachSign(w, s.entity, message, nil)
}
