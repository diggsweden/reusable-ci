#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Validates that the GPG public key secret is configured
# Usage: validate-gpg-public-key.sh
# Expects: OSPO_BOT_GPG_PUB environment variable

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  if [[ -z "${OSPO_BOT_GPG_PUB:-}" ]]; then
    ci_log_error "Missing OSPO_BOT_GPG_PUB secret"
    printf "This secret is needed for GPG operations and signing\n"
    printf "Add it in Settings → Secrets → Actions\n"
    exit 1
  fi

  printf "✓ GPG public key configured\n"
}

main "$@"
