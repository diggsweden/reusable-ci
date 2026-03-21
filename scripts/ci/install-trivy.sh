#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Install Trivy vulnerability scanner if not already available.
#
# Usage: source this file, then call install_trivy
#   source "$(dirname "$0")/../ci/install-trivy.sh"
#   install_trivy

# renovate: datasource=github-releases depName=aquasecurity/trivy
readonly TRIVY_VERSION="v0.62.1"

install_trivy() {
  if command -v trivy &>/dev/null; then
    printf "✅ Trivy already installed: %s\n\n" "$(trivy --version 2>/dev/null | head -1)"
    return
  fi

  local install_dir="${CI_TEMP_DIR:-/tmp}/trivy-bin"
  mkdir -p "$install_dir"

  printf "Installing Trivy vulnerability scanner (version: %s)...\n" "${TRIVY_VERSION}"
  if ! curl -sSfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b "$install_dir" "${TRIVY_VERSION}"; then
    printf "ERROR: Failed to install Trivy %s\n" "${TRIVY_VERSION}" >&2
    return 1
  fi

  export PATH="${install_dir}:$PATH"

  if ! command -v trivy &>/dev/null; then
    printf "ERROR: Trivy binary not found after install\n" >&2
    return 1
  fi

  printf "✅ Trivy installed successfully\n\n"
}
