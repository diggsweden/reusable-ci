#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Rename extracted binaries with a -linux-${arch} suffix so per-arch
# artefacts don't collide on basename when attached to a GitHub Release.
#
# Convention matches Go's release-asset naming
# (e.g., hsm-worker-linux-amd64, hsm-worker-linux-arm64).
#
# Usage: suffix-extracted-binaries.sh DIR ARCH [EXPECTED_NAMES]
#   DIR              Directory holding the extracted binaries (typically
#                    ./extracted-binaries from buildx --output type=local).
#   ARCH             Arch token (e.g., amd64, arm64) appended after `linux-`.
#   EXPECTED_NAMES   Optional comma-separated list of binary basenames to
#                    rename. When empty, every top-level file is renamed.
#
# Examples:
#   suffix-extracted-binaries.sh ./extracted-binaries amd64
#   suffix-extracted-binaries.sh ./extracted-binaries arm64 "hsm-worker,digg-hsm-keytool"

set -euo pipefail

rename_one() {
  local dir="$1"
  local name="$2"
  local arch="$3"
  if [[ -f "${dir}/${name}" ]]; then
    mv "${dir}/${name}" "${dir}/${name}-linux-${arch}"
    printf "renamed %s -> %s-linux-%s\n" "$name" "$name" "$arch"
  fi
}

main() {
  local DIR="${1:?DIR is required}"
  local ARCH="${2:?ARCH is required}"
  local EXPECTED_NAMES="${3:-}"

  if [[ ! -d "$DIR" ]]; then
    printf "error: directory not found: %s\n" "$DIR" >&2
    exit 1
  fi

  if [[ -n "$EXPECTED_NAMES" ]]; then
    local NAMES
    IFS=',' read -ra NAMES <<<"$EXPECTED_NAMES"
    local name trimmed
    for name in "${NAMES[@]}"; do
      trimmed=$(printf '%s' "$name" | xargs)
      [[ -z "$trimmed" ]] && continue
      rename_one "$DIR" "$trimmed" "$ARCH"
    done
  else
    local f base
    for f in "$DIR"/*; do
      [[ -f "$f" ]] || continue
      base=$(basename "$f")
      rename_one "$DIR" "$base" "$ARCH"
    done
  fi
}

main "$@"
