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
readonly MISE_SHA256_LINUX_AMD64="96a0eefa1ad8c92461c808e6e07644f95cda830d7719895b98c819c94b0e0b1c"
readonly MISE_SHA256_LINUX_ARM64="49e2d71d72d68dcb5d554724602d69f33358e1060a0352e5b02b1afcbeecc9b6"
readonly MISE_SHA256_DARWIN_AMD64="a0f0c7119180907951542832202fe21fc8a58e78e7605a7cf92467f64848f5e1"
readonly MISE_SHA256_DARWIN_ARM64="61e1825fc2f5ca8fab6415c726451f6fae47d9e8eb915ad345a76fcfc2ff7315"

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

resolve_mise_sha256() {
	case "$1" in
	mise-v2026.5.4-linux-x64) printf '%s' "$MISE_SHA256_LINUX_AMD64" ;;
	mise-v2026.5.4-linux-arm64) printf '%s' "$MISE_SHA256_LINUX_ARM64" ;;
	mise-v2026.5.4-macos-x64) printf '%s' "$MISE_SHA256_DARWIN_AMD64" ;;
	mise-v2026.5.4-macos-arm64) printf '%s' "$MISE_SHA256_DARWIN_ARM64" ;;
	*)
		printf 'ERROR: no pinned SHA-256 for mise asset %s\n' "$1" >&2
		return 1
		;;
	esac
}

install_mise() {
	if command -v mise &>/dev/null; then
		ci_print_already_installed "mise" "$(mise --version 2>/dev/null | tr -d '\r')"
		return 0
	fi

	local install_dir dist sha256 asset_url download
	install_dir="$(ci_install_dir mise)"
	mkdir -p "$install_dir"

	dist="$(resolve_mise_dist)" || return 1
	sha256="$(resolve_mise_sha256 "$dist")" || return 1
	asset_url="https://github.com/jdx/mise/releases/download/${MISE_VERSION}/${dist}"
	download="$(mktemp "$install_dir/.mise-download.XXXXXXXX")"

	printf "Installing mise (version: %s)...\n" "$MISE_VERSION"
	if ! ci_download_verified "$asset_url" "$download" "$sha256"; then
		rm -f "$download"
		printf "ERROR: Failed to download mise from %s\n" "$asset_url" >&2
		return 1
	fi

	install -m 0755 "$download" "$install_dir/mise"
	rm -f "$download"
	ci_prepend_path "$install_dir"

	ci_require_command mise mise || return 1
	ci_print_installed "mise" "$(mise --version 2>/dev/null | tr -d '\r')"
}
