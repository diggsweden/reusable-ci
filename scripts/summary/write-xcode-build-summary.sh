#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Generate Xcode build summary for GitHub Actions step summary
#
# Required env: XCODE_VERSION, SCHEME
# Optional env: CONFIGURATION, DESTINATION, SIGNING, VERSION, BUILD_NUMBER, IPA_NAME

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

readonly XCODE_VERSION="${XCODE_VERSION:?XCODE_VERSION is required}"
readonly SCHEME="${SCHEME:?SCHEME is required}"
readonly CONFIGURATION="${CONFIGURATION:-Release}"
readonly DESTINATION="${DESTINATION:-generic/platform=iOS}"
readonly SIGNING="${SIGNING:-true}"
readonly VERSION="${VERSION:-unknown}"
readonly BUILD_NUMBER="${BUILD_NUMBER:-unknown}"
readonly IPA_NAME="${IPA_NAME:-}"

generate_summary() {
  printf "## Xcode Build Summary 📱\n\n"

  printf "### Configuration\n"
  printf "| Setting | Value |\n"
  printf "|---------|-------|\n"
  printf "| **Xcode** | %s |\n" "$XCODE_VERSION"
  printf "| **Scheme** | %s |\n" "$SCHEME"
  printf "| **Configuration** | %s |\n" "$CONFIGURATION"
  printf "| **Destination** | %s |\n" "$DESTINATION"
  printf "| **Signing** | %s |\n" "$(ci_bool_status "$SIGNING")"

  if [[ -n "$VERSION" && "$VERSION" != "unknown" ]]; then
    printf "| **Version** | %s (%s) |\n" "$VERSION" "$BUILD_NUMBER"
  fi

  printf "\n### Artifacts Generated\n"

  if [[ "$SIGNING" == "true" ]]; then
    printf "✓ IPA: \`%s\`\n" "$IPA_NAME"
  else
    printf "✓ Archive: \`%s-archive\`\n" "$IPA_NAME"
  fi

  printf "\n*Build completed at %s*\n" "$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
}

main() {
  generate_summary
}

main "$@"
