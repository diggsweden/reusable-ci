#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

# Install GitHub CLI (gh) if not already available.
#
# Usage: source this file, then call install_gh
#   source "$(dirname "$0")/../bootstrap/install-gh.sh"
#   install_gh

# shellcheck source=install-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-common.sh"

# renovate: datasource=github-releases depName=cli/cli
readonly GH_VERSION="v2.93.0"

resolve_gh_dist() {
  local os arch dist=""
  os="${OS:-$(uname -s)}"
  arch="${ARCH:-$(uname -m)}"

  case "$os" in
  Linux)
    case "$arch" in
    x86_64 | amd64) dist="gh_${GH_VERSION#v}_linux_amd64.tar.gz" ;;
    aarch64 | arm64) dist="gh_${GH_VERSION#v}_linux_arm64.tar.gz" ;;
    esac
    ;;
  Darwin)
    case "$arch" in
    x86_64 | amd64) dist="gh_${GH_VERSION#v}_macOS_amd64.zip" ;;
    aarch64 | arm64) dist="gh_${GH_VERSION#v}_macOS_arm64.zip" ;;
    esac
    ;;
  esac

  if [[ -z "$dist" ]]; then
    printf "ERROR: Unsupported gh platform: %s/%s\n" "$os" "$arch" >&2
    return 1
  fi

  printf '%s' "$dist"
}

install_gh() {
  if command -v gh &>/dev/null; then
    ci_print_already_installed "gh" "$(gh --version 2>/dev/null | head -1 | tr -d '\r')"
    return 0
  fi

  local install_dir dist asset_url archive
  install_dir="$(ci_install_dir gh)"
  mkdir -p "$install_dir"

  dist="$(resolve_gh_dist)" || return 1
  asset_url="https://github.com/cli/cli/releases/download/${GH_VERSION}/${dist}"
  archive="$install_dir/${dist}"

  printf "Installing gh (version: %s)...\n" "$GH_VERSION"
  if ! curl --retry 5 --retry-delay 3 --retry-connrefused -fsSL -o "$archive" "$asset_url"; then
    printf "ERROR: Failed to download gh from %s\n" "$asset_url" >&2
    return 1
  fi

  # The tarball expands to gh_<ver>_<os>_<arch>/bin/gh; flatten so the
  # binary ends up directly at $install_dir/gh.
  tar -xzf "$archive" -C "$install_dir"
  local extracted_root
  extracted_root="$install_dir/${dist%.tar.gz}"
  mv "$extracted_root/bin/gh" "$install_dir/gh"
  rm -rf "$extracted_root" "$archive"
  chmod +x "$install_dir/gh"
  ci_prepend_path "$install_dir"

  ci_require_command gh gh || return 1
  ci_print_installed "gh" "$(gh --version 2>/dev/null | head -1 | tr -d '\r')"
}
