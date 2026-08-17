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
// are preserved untouched.
//
// The credential is stored as base64(username:password) — the standard,
// tool-neutral encoding. Note this is encoding, NOT encryption: the file is
// only as protected as its permissions, so callers must write it 0600.
func MergeAuth(existing []byte, registry, username, password string) ([]byte, error) {
	if registry == "" || username == "" || password == "" {
		return nil, fmt.Errorf("registry, username, and password are all required: %w", errs.ErrUsage)
	}

	root := map[string]any{}
	if len(bytes.TrimSpace(existing)) > 0 {
		if err := json.Unmarshal(existing, &root); err != nil {
			return nil, fmt.Errorf("parse existing auth config: %w", errs.ErrMalformedInput)
		}
	}

	auths, _ := root["auths"].(map[string]any)
	if auths == nil {
		auths = map[string]any{}
	}

	entry, _ := auths[registry].(map[string]any)
	if entry == nil {
		entry = map[string]any{}
	}

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
func RemoveAuth(existing []byte, registry string) ([]byte, bool, error) {
	if registry == "" {
		return nil, false, fmt.Errorf("registry is required: %w", errs.ErrUsage)
	}

	if len(bytes.TrimSpace(existing)) == 0 {
		return existing, false, nil
	}

	root := map[string]any{}
	if err := json.Unmarshal(existing, &root); err != nil {
		return nil, false, fmt.Errorf("parse existing auth config: %w", errs.ErrMalformedInput)
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
