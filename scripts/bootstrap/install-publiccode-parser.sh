#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

# Install the publiccode-parser-go CLI if not already available.
#
# Usage: source this file, then call install_publiccode_parser
#   source "$(dirname "$0")/../bootstrap/install-publiccode-parser.sh"
#   install_publiccode_parser

# shellcheck source=install-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-common.sh"

# renovate: datasource=github-releases depName=italia/publiccode-parser-go
readonly PUBLICCODE_PARSER_VERSION="v5.3.1"

resolve_publiccode_parser_dist() {
  local os arch dist=""
  os="${OS:-$(uname -s)}"
  arch="${ARCH:-$(uname -m)}"

  case "$os" in
  Linux)
    case "$arch" in
    x86_64 | amd64) dist="publiccode-parser-go_Linux_x86_64.tar.gz" ;;
    aarch64 | arm64) dist="publiccode-parser-go_Linux_arm64.tar.gz" ;;
    esac
    ;;
  Darwin)
    case "$arch" in
    x86_64 | amd64) dist="publiccode-parser-go_Darwin_x86_64.tar.gz" ;;
    aarch64 | arm64) dist="publiccode-parser-go_Darwin_arm64.tar.gz" ;;
    esac
    ;;
  esac

  if [[ -z "$dist" ]]; then
    printf "ERROR: Unsupported publiccode-parser platform: %s/%s\n" "$os" "$arch" >&2
    return 1
  fi

  printf '%s' "$dist"
}

install_publiccode_parser() {
  if command -v publiccode-parser &>/dev/null; then
    ci_print_already_installed "publiccode-parser" "$(publiccode-parser --version 2>/dev/null | head -1 | tr -d '\r')"
    return 0
  fi

  local install_dir dist asset_url archive
  install_dir="$(ci_install_dir publiccode-parser)"
  mkdir -p "$install_dir"

  dist="$(resolve_publiccode_parser_dist)" || return 1
  asset_url="https://github.com/italia/publiccode-parser-go/releases/download/${PUBLICCODE_PARSER_VERSION}/${dist}"
  archive="$install_dir/${dist}"

  printf "Installing publiccode-parser (version: %s)...\n" "$PUBLICCODE_PARSER_VERSION"
  if ! curl --retry 5 --retry-delay 3 --retry-connrefused -fsSL -o "$archive" "$asset_url"; then
    printf "ERROR: Failed to download publiccode-parser from %s\n" "$asset_url" >&2
    return 1
  fi

  tar -xzf "$archive" -C "$install_dir"
  rm -f "$archive"
  chmod +x "$install_dir/publiccode-parser"
  ci_prepend_path "$install_dir"

  ci_require_command publiccode-parser publiccode-parser || return 1
  ci_print_installed "publiccode-parser" "$(publiccode-parser --version 2>/dev/null | head -1 | tr -d '\r')"
}
