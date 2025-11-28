#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Generate Xcode build summary for GitHub Actions step summary
# Usage: xcode-build-summary.sh <xcode-version> <scheme> <configuration> <destination> \
#        <signing> <version> <build> <ipa-name>

set -euo pipefail

readonly XCODE_VERSION="${1:?Usage: $0 <xcode-version> <scheme> <configuration> <destination> <signing> <version> <build> <ipa-name>}"
readonly SCHEME="${2:?}"
readonly CONFIGURATION="${3:-Release}"
readonly DESTINATION="${4:-generic/platform=iOS}"
readonly SIGNING="${5:-true}"
readonly VERSION="${6:-unknown}"
readonly BUILD="${7:-unknown}"
readonly IPA_NAME="${8:-}"

bool_status() {
  [[ "$1" == "true" ]] && printf "✓ Enabled" || printf "⊘ Disabled"
}

generate_summary() {
  printf "## Xcode Build Summary 📱\n\n"

  printf "### Configuration\n"
  printf "| Setting | Value |\n"
  printf "|---------|-------|\n"
  printf "| **Xcode** | %s |\n" "$XCODE_VERSION"
  printf "| **Scheme** | %s |\n" "$SCHEME"
  printf "| **Configuration** | %s |\n" "$CONFIGURATION"
  printf "| **Destination** | %s |\n" "$DESTINATION"
  printf "| **Signing** | %s |\n" "$(bool_status "$SIGNING")"

  if [[ -n "$VERSION" && "$VERSION" != "unknown" ]]; then
    printf "| **Version** | %s (%s) |\n" "$VERSION" "$BUILD"
  fi

  printf "\n### Artifacts Generated\n"

  if [[ "$SIGNING" == "true" ]]; then
    printf "- ✓ IPA: \`%s\`\n" "$IPA_NAME"
  else
    printf "- ✓ Archive: \`%s-archive\`\n" "$IPA_NAME"
  fi

  printf "\n*Build completed at %s*\n" "$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
}

generate_summary
