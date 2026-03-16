#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Validates that the workflow was triggered by a tag push
# Usage: validate-ref-type.sh <ref-type> <ref-name> [ref]

set -euo pipefail

main() {
  local REF_TYPE="${1:-}"
  local REF_NAME="${2:-}"
  local REF="${3:-}"

  if [[ -z "$REF_TYPE" ]]; then
    printf "::error::Usage: %s <ref-type> <ref-name> [ref]\n" "$0"
    exit 1
  fi

  if [[ "$REF_TYPE" != "tag" ]]; then
    printf "::error::Release workflow must be triggered by pushing a tag\n"
    printf "::error::Current trigger: %s (%s)\n" "$REF_TYPE" "$REF"
    printf "::error::To create a release, push a signed tag:\n"
    printf "::error::  git tag -s v1.0.0 -m 'Release v1.0.0'\n"
    printf "::error::  git push origin v1.0.0\n"
    exit 1
  fi

  printf "✓ Triggered by tag: %s\n" "$REF_NAME"
}

main "$@"
