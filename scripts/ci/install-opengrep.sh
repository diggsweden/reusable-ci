#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Install OpenGrep if not already available.
#
# Usage: source this file, then call install_opengrep
#   source "$(dirname "$0")/../ci/install-opengrep.sh"
#   install_opengrep

# renovate: datasource=github-releases depName=opengrep/opengrep
readonly OPENGREP_VERSION="v1.18.0"

resolve_opengrep_dist() {
  local os arch dist=""
  os="${OS:-$(uname -s)}"
  arch="${ARCH:-$(uname -m)}"

  case "$os" in
  Linux)
    if ldd /bin/sh 2>&1 | grep -qi musl; then
      case "$arch" in
      x86_64 | amd64) dist="opengrep_musllinux_x86" ;;
      aarch64 | arm64) dist="opengrep_musllinux_aarch64" ;;
      esac
    else
      case "$arch" in
      x86_64 | amd64) dist="opengrep_manylinux_x86" ;;
      aarch64 | arm64) dist="opengrep_manylinux_aarch64" ;;
      esac
    fi
    ;;
  Darwin)
    case "$arch" in
    x86_64 | amd64) dist="opengrep_osx_x86" ;;
    aarch64 | arm64) dist="opengrep_osx_arm64" ;;
    esac
    ;;
  esac

  if [[ -z "$dist" ]]; then
    printf "ERROR: Unsupported OpenGrep platform: %s/%s\n" "$os" "$arch" >&2
    return 1
  fi

  printf '%s' "$dist"
}

install_opengrep() {
  if command -v opengrep &>/dev/null; then
    printf "OpenGrep already installed: %s\n\n" "$(opengrep --version 2>/dev/null | tr -d '\r')"
    return 0
  fi

  local install_dir dist asset_url
  install_dir="${CI_TEMP_DIR:-/tmp}/opengrep-bin"
  mkdir -p "$install_dir"

  dist="$(resolve_opengrep_dist)" || return 1
  asset_url="https://github.com/opengrep/opengrep/releases/download/${OPENGREP_VERSION}/${dist}"

  printf "Installing OpenGrep (version: %s)...\n" "$OPENGREP_VERSION"
  if ! curl -fsSL -o "$install_dir/opengrep" "$asset_url"; then
    printf "ERROR: Failed to download OpenGrep from %s\n" "$asset_url" >&2
    return 1
  fi

  chmod +x "$install_dir/opengrep"
  export PATH="${install_dir}:$PATH"

  if ! command -v opengrep &>/dev/null; then
    printf "ERROR: OpenGrep binary not found after install\n" >&2
    return 1
  fi

  printf "OpenGrep installed successfully: %s\n\n" "$(opengrep --version 2>/dev/null | tr -d '\r')"
}
