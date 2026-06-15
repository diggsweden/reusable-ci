// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// MergeAuth adds (or replaces) the credential for registry in a docker/OCI auth
// config document — the {"auths": {"<registry>": {"auth": "<b64>"}}} format read
// by docker, podman/buildah, skopeo, and cosign alike, so one written file
// authenticates every tool on both ghcr and Codeberg/Forgejo. existing may be
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
