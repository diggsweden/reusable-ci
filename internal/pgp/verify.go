// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package pgp verifies signatures and parses key bundles without filesystem or process access.
package pgp

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"io"
	"strings"
)

// Static parse failures of a keyring bundle. Both are wrapped by
// callers with errs.ErrMalformedInput, which is what distinguishes a
// broken trust anchor from a rejected signature.
var (
	errNoArmoredData = errors.New("no armored data found")
	errNotAKeyBlock  = errors.New("expected a public or private key block")
)

// readArmoredKeyRingAll reads every armor block in a concatenated
// public-key bundle, not just the first.
//
// go-crypto's ReadArmoredKeyRing decodes exactly one armor block, so a
// file built the way docs/verification.md tells operators to build it —
// `gpg --armor --export <email> >> allowed_gpg_keys.asc` — yields a
// keyring holding only the keys of the first block. Every later block is
// silently invisible: the allowlist is narrower than the file, and the
// key count in the refusal message is the only hint. That bites during
// key rotation, the one time the file deliberately holds two keys.
//
// Reading every block is what the documented file format already
// promises ("one or more PGP PUBLIC KEY BLOCK sections concatenated").
// It widens nothing beyond that promise: the keys still have to be
// committed and reviewed to be in the file at all.
//
// Trailing non-armor bytes end the scan without error, matching armor's
// own tolerance for surrounding text.
func readArmoredKeyRingAll(armored []byte) (openpgp.EntityList, error) {
	blocks := SplitArmorBlocks(armored)
	if len(blocks) == 0 {
		// Matches ReadArmoredKeyRing: a file with bytes but no armor is
		// a broken keyring, never an empty allowlist.
		return nil, errNoArmoredData
	}

	var all openpgp.EntityList

	for _, block := range blocks {
		decoded, err := armor.Decode(bytes.NewReader(block))
		if err != nil {
			// A malformed block is reported even when earlier blocks
			// parsed: reading a file in part is the defect this
			// function exists to remove, not a behaviour to keep.
			return nil, err
		}

		if decoded.Type != openpgp.PublicKeyType && decoded.Type != openpgp.PrivateKeyType {
			return nil, fmt.Errorf("%w, got: %s", errNotAKeyBlock, decoded.Type)
		}

		list, err := openpgp.ReadKeyRing(decoded.Body)
		if err != nil {
			return nil, err
		}

		all = append(all, list...)
	}

	return all, nil
}

// SplitArmorBlocks cuts a concatenated bundle into one byte slice per
// armor block, each decodable on its own.
//
// Splitting is necessary rather than tidy: armor.Decode wraps its input
// in a bufio.Reader, so it reads past the block it returns. A second
// Decode against the same reader therefore always reports EOF, which is
// why decoding in a loop silently yields only the first block.
//
// Blocks are cut at the closing dashes of the END line rather than at a
// newline, because a bundle built by concatenating exports need not have
// one between blocks. A trailing BEGIN with no END is kept whole so
// armor.Decode reports it as malformed; dropping it here would be the
// same silent partial read in a new place. Text between and around
// blocks is ignored, as armor itself ignores it.
func SplitArmorBlocks(armored []byte) [][]byte {
	const (
		beginMarker = "-----BEGIN "
		endMarker   = "-----END "
		dashes      = "-----"
	)

	var blocks [][]byte

	rest := armored

	for {
		begin := bytes.Index(rest, []byte(beginMarker))
		if begin < 0 {
			return blocks
		}

		rest = rest[begin:]

		end := bytes.Index(rest, []byte(endMarker))
		if end < 0 {
			return append(blocks, rest)
		}

		afterMarker := end + len(endMarker)

		tail := bytes.Index(rest[afterMarker:], []byte(dashes))
		if tail < 0 {
			return append(blocks, rest)
		}

		cut := afterMarker + tail + len(dashes)
		if cut < len(rest) && rest[cut] == '\n' {
			cut++
		}

		blocks = append(blocks, rest[:cut])
		rest = rest[cut:]
	}
}

// PrimaryFingerprints parses an armored public-key bundle (one or more
// "PGP PUBLIC KEY BLOCK" sections concatenated) and returns the
// uppercase-hex primary-key fingerprint of every key in it: 40 chars
// for a v4 key, 64 for a v6 one (RFC 9580).
//
// It turns a committed `.reusable-ci/allowed_gpg_keys.asc` keyring into
// the GPG signer allowlist: the same file is both the verification key
// material (passed to VerifyTagSignature) and the set of authorised
// fingerprints, so the two can never drift apart.
//
// Empty armor → empty slice, nil error (caller treats "no keys" as
// "no allowlist from this source").
func PrimaryFingerprints(armor []byte) ([]string, error) {
	armor = bytes.TrimSpace(armor)
	if len(armor) == 0 {
		return nil, nil
	}

	list, err := readArmoredKeyRingAll(armor)
	if err != nil {
		return nil, fmt.Errorf("parse armored key ring: %w: %w", err, errs.ErrMalformedInput)
	}

	fps := make([]string, 0, len(list))
	for _, entity := range list {
		if entity.PrimaryKey == nil {
			continue
		}

		fps = append(fps, strings.ToUpper(fmt.Sprintf("%X", entity.PrimaryKey.Fingerprint)))
	}

	return fps, nil
}

// VerifyDetachedArmored verifies an armored detached signature
// against an artifact, using the supplied armored public key(s) as
// the trust anchor. Wraps go-crypto's CheckArmoredDetachedSignature
// so the validate flow doesn't need to import openpgp directly.
//
// Returns nil when the signature verifies against any entity in
// pubKeyArmor. Any failure (parse, verify, IO) is wrapped with
// errs.ErrPermissionDenied so callers can detect verification
// failures distinct from infrastructure errors.
func VerifyDetachedArmored(artifact io.Reader, signature io.Reader, pubKeyArmor []byte) error {
	keyring, err := readArmoredKeyRingAll(pubKeyArmor)
	if err != nil {
		return fmt.Errorf("parse armored public key: %w: %w", err, errs.ErrMalformedInput)
	}

	if len(keyring) == 0 {
		return fmt.Errorf("public key armor is empty: %w", errs.ErrMissingInput)
	}

	if _, err := openpgp.CheckArmoredDetachedSignature(keyring, artifact, signature, nil); err != nil {
		return fmt.Errorf("verify detached signature: %w: %w", err, errs.ErrPermissionDenied)
	}

	return nil
}
