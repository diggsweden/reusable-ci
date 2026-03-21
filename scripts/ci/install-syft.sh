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

  local install_dir="${CI_TEMP_DIR:-/tmp}/syft-bin"
  mkdir -p "$install_dir"

  printf "Installing Syft SBOM generator (version: %s)...\n" "${SYFT_VERSION}"
  if ! curl -sSfL https://raw.githubusercontent.com/anchore/syft/main/install.sh | sh -s -- -b "$install_dir" "${SYFT_VERSION}"; then
    printf "ERROR: Failed to install Syft %s\n" "${SYFT_VERSION}" >&2
    return 1
  fi

  export PATH="${install_dir}:$PATH"
  printf "✅ Syft installed successfully\n\n"
}
