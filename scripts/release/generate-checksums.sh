#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  local OUTPUT_FILE="${1:-$CI_CHECKSUMS_FILE}"
  local RELEASE_ARTIFACTS_DIR="${2:-./release-artifacts}"
  local ATTACH_PATTERNS="${3:-}"
  local SBOM_DIR="${4:-./sbom-artifacts}"

  printf "Generating SHA256 checksums for release artifacts\n"

  touch "$OUTPUT_FILE"

  if [[ -d "$RELEASE_ARTIFACTS_DIR" ]]; then
    printf "→ Checksumming release artifacts from %s\n" "$RELEASE_ARTIFACTS_DIR"
    for file in "$RELEASE_ARTIFACTS_DIR"/*; do
      if [[ -f "$file" ]]; then
        sha256sum "$file" | sed "s|$RELEASE_ARTIFACTS_DIR/||" >>"$OUTPUT_FILE"
      fi
    done
  fi

  if [[ -n "$ATTACH_PATTERNS" ]]; then
    printf "→ Checksumming attached artifacts matching patterns: %s\n" "$ATTACH_PATTERNS"
    local PATTERNS
    IFS=',' read -ra PATTERNS <<<"$ATTACH_PATTERNS"
    for pattern in "${PATTERNS[@]}"; do
      pattern=$(printf "%s" "$pattern" | xargs)
      for file in $pattern; do
        if [[ -f "$file" ]]; then
          sha256sum "$file" >>"$OUTPUT_FILE"
        fi
      done
    done
  fi

  if [[ -d "$SBOM_DIR" ]]; then
    printf "→ Checksumming container SBOMs from %s\n" "$SBOM_DIR"
    for file in "$SBOM_DIR"/*-container-sbom.*.json; do
      if [[ -f "$file" ]]; then
        local filename
        filename=$(basename "$file")
        sha256sum "$file" | sed "s|$file|$filename|" >>"$OUTPUT_FILE"
      fi
    done
  fi

  printf "→ Checksumming all SBOM layers\n"
  for sbom in *-sbom.spdx.json *-sbom.cyclonedx.json; do
    if [[ -f "$sbom" ]]; then
      sha256sum "$sbom" >>"$OUTPUT_FILE"
    fi
  done

  local CHECKSUM_COUNT
  CHECKSUM_COUNT=$(wc -l <"$OUTPUT_FILE")
  printf "✓ Generated %s checksums in %s\n" "$CHECKSUM_COUNT" "$OUTPUT_FILE"
}

main "$@"
