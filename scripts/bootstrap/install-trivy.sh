#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

# Install Trivy vulnerability scanner if not already available.
#
# Usage: source this file, then call install_trivy
#   source "$(dirname "$0")/../bootstrap/install-trivy.sh"
#   install_trivy

# shellcheck source=install-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-common.sh"

# renovate: datasource=github-releases depName=aquasecurity/trivy
readonly TRIVY_VERSION="v0.69.3"

install_trivy() {
  if command -v trivy &>/dev/null; then
    ci_print_already_installed "Trivy" "$(trivy --version 2>/dev/null | head -1)"
    return 0
  fi

  local install_dir
  install_dir="$(ci_install_dir trivy)"
  mkdir -p "$install_dir"

  printf "Installing Trivy vulnerability scanner (version: %s)...\n" "${TRIVY_VERSION}"
  if ! curl -sSfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b "$install_dir" "${TRIVY_VERSION}"; then
    printf "ERROR: Failed to install Trivy %s\n" "${TRIVY_VERSION}" >&2
    return 1
  fi

  ci_prepend_path "$install_dir"
  ci_require_command trivy Trivy || return 1
  ci_print_installed "Trivy" "$(trivy --version 2>/dev/null | head -1)"
}
