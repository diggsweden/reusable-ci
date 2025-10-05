// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// MergeAuth adds (or replaces) the credential for registry in a docker/OCI auth
// config document — the {"auths": {"<registry>": {"auth": "<b64>"}}} format read
// by docker, podman/buildah, skopeo, and cosign alike, so one written file
// authenticates every tool on both ghcr and Forgejo. existing may be
// empty (a fresh config) or a prior config, whose other registries and fields
// are preserved, including exact numeric values. Non-object roots are rejected;
// a missing or non-object auths/target entry is replaced with an object.
//
// The credential is stored as base64(username:password) — the standard,
// tool-neutral encoding. Note this is encoding, NOT encryption: the file is
// only as protected as its permissions, so callers must write it 0600.
func MergeAuth(existing []byte, registry, username, password string) ([]byte, error) {
	if registry == "" || username == "" || password == "" {
		return nil, fmt.Errorf("registry, username, and password are all required: %w", errs.ErrUsage)
	}

	root, err := parseAuthConfig(existing)
	if err != nil {
		return nil, err
	}

	auths, _ := root["auths"].(map[string]any)
	if auths == nil {
		auths = map[string]any{}
	}

	entry, _ := auths[registry].(map[string]any)
	if entry == nil {
		entry = map[string]any{}
	}

	// Docker-compatible clients prefer identitytoken over auth. Retaining a
	// token from an earlier login would silently ignore the new credential.
	delete(entry, "identitytoken")
	entry["auth"] = base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	auths[registry] = entry
	root["auths"] = auths

	return json.MarshalIndent(root, "", "  ")
}

// RemoveAuth deletes registry's credential from an OCI auth config document,
// the inverse of MergeAuth. Other registries and fields are preserved. It
// reports whether an entry was actually removed (false when the config is
// empty or holds no credential for registry) so callers can stay idempotent —
// a logout for a registry that was never logged in is a successful no-op, not
// a rewrite. The password is never touched, so nothing sensitive is echoed.
// On a no-op, including missing or non-object auths, existing is returned
// byte-for-byte. Non-object document roots are rejected as in MergeAuth.
func RemoveAuth(existing []byte, registry string) ([]byte, bool, error) {
	if registry == "" {
		return nil, false, fmt.Errorf("registry is required: %w", errs.ErrUsage)
	}

	root, err := parseAuthConfig(existing)
	if err != nil {
		return nil, false, err
	}

	auths, _ := root["auths"].(map[string]any)
	if _, ok := auths[registry]; !ok {
		return existing, false, nil
	}

	delete(auths, registry)
	root["auths"] = auths

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("encode auth config: %w", err)
	}

	return out, true, nil
}

func parseAuthConfig(existing []byte) (map[string]any, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(existing)) == 0 {
		return root, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(existing))
	decoder.UseNumber()
	// Unlike Unmarshal, Decode accepts trailing documents; require only JSON whitespace.
	if err := decoder.Decode(&root); err != nil || root == nil || len(bytes.Trim(existing[decoder.InputOffset():], " \t\r\n")) != 0 {
		return nil, fmt.Errorf("parse existing auth config: %w", errs.ErrMalformedInput)
	}

	return root, nil
}
