#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  local sarif_file tmp_file
  sarif_file="${SARIF_FILE:-}"
  tmp_file=""

  if [[ -z "$sarif_file" ]]; then
    ci_log_error "SARIF_FILE environment variable is required"
    exit 1
  fi

  if [[ ! -f "$sarif_file" ]]; then
    ci_log_warning "SARIF file not found, skipping GitHub enrichment: ${sarif_file}"
    exit 0
  fi

  if ! command -v jq &>/dev/null; then
    ci_log_warning "jq not available, skipping GitHub SARIF enrichment"
    exit 0
  fi

  tmp_file="$(mktemp "${CI_TEMP_DIR:-/tmp}/opengrep-sarif.XXXXXX")"
  trap '[[ -n "${tmp_file:-}" ]] && rm -f "$tmp_file"' EXIT

  jq '
    (.runs[]?.results[]?) |= (
      if ((.partialFingerprints.primaryLocationLineHash? // "") != "") then
        .
      else
        .partialFingerprints = (
          (.partialFingerprints // {}) + {
            "primaryLocationLineHash": (
              .fingerprints["matchBasedId/v1"]
              // ([
                .ruleId // "rule",
                .locations[0].physicalLocation.artifactLocation.uri // "unknown",
                ((.locations[0].physicalLocation.region.startLine // 0) | tostring),
                .message.text // ""
              ] | join("|"))
            )
          }
        )
      end
    )
  ' "$sarif_file" >"$tmp_file"

  mv "$tmp_file" "$sarif_file"
  printf "Enriched SARIF with GitHub partialFingerprints: %s\n" "$sarif_file"
}

main "$@"
