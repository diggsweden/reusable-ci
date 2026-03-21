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

  printf "Installing Trivy vulnerability scanner (version: %s)...\n" "${TRIVY_VERSION}"
  curl -sSfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b /tmp "${TRIVY_VERSION}"
  export PATH="/tmp:$PATH"
  printf "✅ Trivy installed successfully\n\n"
}
