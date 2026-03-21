#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Validates that the workflow was triggered by a tag push
# Usage: ref-type.sh <ref-type> <ref-name> [ref]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  local REF_TYPE="${1:-}"
  local REF_NAME="${2:-}"
  local REF="${3:-}"

  if [[ -z "$REF_TYPE" ]]; then
    ci_log_error "Usage: $0 <ref-type> <ref-name> [ref]"
    exit 1
  fi

  if [[ "$REF_TYPE" != "tag" ]]; then
    ci_log_error "Release workflow must be triggered by pushing a tag"
    ci_log_error "Current trigger: $REF_TYPE ($REF)"
    ci_log_error "To create a release, push a signed tag:"
    ci_log_error "  git tag -s v1.0.0 -m 'Release v1.0.0'"
    ci_log_error "  git push origin v1.0.0"
    exit 1
  fi

  printf "✓ Triggered by tag: %s\n" "$REF_NAME"
}

main "$@"
