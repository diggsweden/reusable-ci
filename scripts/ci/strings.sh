#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Shared string-sanitization helpers
#
# One regex, one place. Kept in scripts/ci/ alongside env.sh / output.sh so
# both pipeline scripts (sbom, version, release) and tests can source it.

[[ -n "${_CI_STRINGS_LOADED:-}" ]] && return 0
_CI_STRINGS_LOADED=1

# sanitize_path_token <input>
#
# Maps any character outside [a-zA-Z0-9._-] to '-' and strips leading/trailing
# dashes. The result is safe for:
#   - filesystem paths (no '/', no NUL, no spaces)
#   - Docker/OCI tags (the docker reference grammar)
#   - artefact basenames in SBOM filenames (which the file-write boundary
#     interpolates directly into bash redirections)
#
# Idempotent: sanitizing an already-clean token returns it unchanged. Callers
# that pass already-sanitized values (e.g. dev-version) pay no semantic cost.
sanitize_path_token() {
  printf "%s" "$1" | sed 's|[^a-zA-Z0-9._-]|-|g; s|^-*||; s|-*$||'
}
