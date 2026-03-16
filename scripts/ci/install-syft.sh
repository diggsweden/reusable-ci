#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Install Syft SBOM generator if not already available.
#
# Usage: source this file, then call install_syft
#   source "$(dirname "$0")/../ci/install-syft.sh"
#   install_syft

# renovate: datasource=github-releases depName=anchore/syft
readonly SYFT_VERSION="v1.37.0"

install_syft() {
  if command -v syft &>/dev/null; then
    printf "✅ Syft already installed: %s\n\n" "$(syft version --output json 2>/dev/null | grep -o '"version":"[^"]*"' | cut -d'"' -f4)"
    return
  fi

  printf "Installing Syft SBOM generator (version: %s)...\n" "${SYFT_VERSION}"
  curl -sSfL https://raw.githubusercontent.com/anchore/syft/main/install.sh | sh -s -- -b /tmp "${SYFT_VERSION}"
  export PATH="/tmp:$PATH"
  printf "✅ Syft installed successfully\n\n"
}
