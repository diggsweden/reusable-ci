#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Install mise (dev tool version manager) if not already available.
#
# Usage: source this file, then call install_mise
#   source "$(dirname "$0")/../bootstrap/install-mise.sh"
#   install_mise

# shellcheck source=install-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-common.sh"

# renovate: datasource=github-releases depName=jdx/mise
readonly MISE_VERSION="v2026.5.4"

resolve_mise_dist() {
	local os arch dist=""
	os="${OS:-$(uname -s)}"
	arch="${ARCH:-$(uname -m)}"

	case "$os" in
	Linux)
		case "$arch" in
		x86_64 | amd64) dist="mise-${MISE_VERSION}-linux-x64" ;;
		aarch64 | arm64) dist="mise-${MISE_VERSION}-linux-arm64" ;;
		esac
		;;
	Darwin)
		case "$arch" in
		x86_64 | amd64) dist="mise-${MISE_VERSION}-macos-x64" ;;
		aarch64 | arm64) dist="mise-${MISE_VERSION}-macos-arm64" ;;
		esac
		;;
	esac

	if [[ -z "$dist" ]]; then
		printf "ERROR: Unsupported mise platform: %s/%s\n" "$os" "$arch" >&2
		return 1
	fi

	printf '%s' "$dist"
}

install_mise() {
	if command -v mise &>/dev/null; then
		ci_print_already_installed "mise" "$(mise --version 2>/dev/null | tr -d '\r')"
		return 0
	fi

	local install_dir dist asset_url
	install_dir="$(ci_install_dir mise)"
	mkdir -p "$install_dir"

	dist="$(resolve_mise_dist)" || return 1
	asset_url="https://github.com/jdx/mise/releases/download/${MISE_VERSION}/${dist}"

	printf "Installing mise (version: %s)...\n" "$MISE_VERSION"
	if ! curl --retry 5 --retry-delay 3 --retry-connrefused -fsSL -o "$install_dir/mise" "$asset_url"; then
		printf "ERROR: Failed to download mise from %s\n" "$asset_url" >&2
		return 1
	fi

	chmod +x "$install_dir/mise"
	ci_prepend_path "$install_dir"

	ci_require_command mise mise || return 1
	ci_print_installed "mise" "$(mise --version 2>/dev/null | tr -d '\r')"
}
