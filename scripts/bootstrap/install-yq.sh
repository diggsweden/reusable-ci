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

install_yq() {
	if command -v yq &>/dev/null; then
		ci_print_already_installed "yq" "$(yq --version 2>/dev/null | tr -d '\r')"
		return 0
	fi

	local install_dir dist asset_url
	install_dir="$(ci_install_dir yq)"
	mkdir -p "$install_dir"

	dist="$(resolve_yq_dist)" || return 1
	asset_url="https://github.com/mikefarah/yq/releases/download/${YQ_VERSION}/${dist}"

	printf "Installing yq (version: %s)...\n" "$YQ_VERSION"
	if ! curl --retry 5 --retry-delay 3 --retry-connrefused -fsSL -o "$install_dir/yq" "$asset_url"; then
		printf "ERROR: Failed to download yq from %s\n" "$asset_url" >&2
		return 1
	fi

	chmod +x "$install_dir/yq"
	ci_prepend_path "$install_dir"

	ci_require_command yq yq || return 1
	ci_print_installed "yq" "$(yq --version 2>/dev/null | tr -d '\r')"
}
