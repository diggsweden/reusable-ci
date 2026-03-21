#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

require_secret() {
  local secret_name="$1"
  local secret_value="$2"

  if [[ -z "$secret_value" ]]; then
    ci_log_error "Missing $secret_name secret"
    printf "Required for publishing to Maven Central\n"
    exit 1
  fi
}

main() {
  readonly MAVENCENTRAL_USERNAME="${MAVENCENTRAL_USERNAME:-}"
  readonly MAVENCENTRAL_PASSWORD="${MAVENCENTRAL_PASSWORD:-}"

  require_secret "MAVENCENTRAL_USERNAME" "$MAVENCENTRAL_USERNAME"
  require_secret "MAVENCENTRAL_PASSWORD" "$MAVENCENTRAL_PASSWORD"

  printf "✓ Maven Central credentials configured\n"
}

main "$@"
