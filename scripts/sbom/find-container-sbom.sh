#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  local sbom_file

  sbom_file=$(find . -maxdepth 1 -name "*-container-sbom.spdx.json" -type f -print -quit)
  if [[ -z "$sbom_file" ]]; then
    ci_log_error "No container SBOM file found matching pattern *-container-sbom.spdx.json"
    ci_log_error "SBOM generation step may have failed"
    exit 1
  fi

  sbom_file=$(basename "$sbom_file")
  ci_output "sbom-file" "$sbom_file"
  printf "Found SBOM file: %s\n" "$sbom_file"
}

main "$@"
