#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Install Mike Farah yq if not already available.
#
# Usage: source this file, then call install_yq
#   source "$(dirname "$0")/../bootstrap/install-yq.sh"
#   install_yq

# shellcheck source=install-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-common.sh"

# renovate: datasource=github-releases depName=mikefarah/yq
readonly YQ_VERSION="v4.52.5"
readonly YQ_SHA256_LINUX_AMD64="75d893a0d5940d1019cb7cdc60001d9e876623852c31cfc6267047bc31149fa9"
readonly YQ_SHA256_LINUX_ARM64="90fa510c50ee8ca75544dbfffed10c88ed59b36834df35916520cddc623d9aaa"
readonly YQ_SHA256_LINUX_ARM="dea10a4f66646160b592bb8ddaed9fc8c5f13d235474e66c47178cbf2f816a73"
readonly YQ_SHA256_DARWIN_AMD64="6e399d1eb466860c3202d231727197fdce055888c5c7bec6964156983dd1559d"
readonly YQ_SHA256_DARWIN_ARM64="45a12e64d4bd8a31c72ee1b889e81f1b1110e801baad3d6f030c111db0068de0"

resolve_yq_dist() {
	local os arch dist=""
	os="${OS:-$(uname -s)}"
	arch="${ARCH:-$(uname -m)}"

	case "$os" in
	Linux)
		case "$arch" in
		x86_64 | amd64) dist="yq_linux_amd64" ;;
		aarch64 | arm64) dist="yq_linux_arm64" ;;
		armv6l | armv7l | arm) dist="yq_linux_arm" ;;
		esac
		;;
	Darwin)
		case "$arch" in
		x86_64 | amd64) dist="yq_darwin_amd64" ;;
		aarch64 | arm64) dist="yq_darwin_arm64" ;;
		esac
		;;
	esac

	if [[ -z "$dist" ]]; then
		printf "ERROR: Unsupported yq platform: %s/%s\n" "$os" "$arch" >&2
		return 1
	fi

	printf '%s' "$dist"
}

resolve_yq_sha256() {
	case "$1" in
	yq_linux_amd64) printf '%s' "$YQ_SHA256_LINUX_AMD64" ;;
	yq_linux_arm64) printf '%s' "$YQ_SHA256_LINUX_ARM64" ;;
	yq_linux_arm) printf '%s' "$YQ_SHA256_LINUX_ARM" ;;
	yq_darwin_amd64) printf '%s' "$YQ_SHA256_DARWIN_AMD64" ;;
	yq_darwin_arm64) printf '%s' "$YQ_SHA256_DARWIN_ARM64" ;;
	*)
		printf 'ERROR: no pinned SHA-256 for yq asset %s\n' "$1" >&2
		return 1
		;;
	esac
}

install_yq() {
	if command -v yq &>/dev/null; then
		ci_print_already_installed "yq" "$(yq --version 2>/dev/null | tr -d '\r')"
		return 0
	fi

	local install_dir dist sha256 asset_url download
	install_dir="$(ci_install_dir yq)"
	mkdir -p "$install_dir"

	dist="$(resolve_yq_dist)" || return 1
	sha256="$(resolve_yq_sha256 "$dist")" || return 1
	asset_url="https://github.com/mikefarah/yq/releases/download/${YQ_VERSION}/${dist}"
	download="$(mktemp "$install_dir/.yq-download.XXXXXXXX")"

	printf "Installing yq (version: %s)...\n" "$YQ_VERSION"
	if ! ci_download_verified "$asset_url" "$download" "$sha256"; then
		rm -f "$download"
		printf "ERROR: Failed to download yq from %s\n" "$asset_url" >&2
		return 1
	fi

	install -m 0755 "$download" "$install_dir/yq"
	rm -f "$download"
	ci_prepend_path "$install_dir"

	ci_require_command yq yq || return 1
	ci_print_installed "yq" "$(yq --version 2>/dev/null | tr -d '\r')"
}
