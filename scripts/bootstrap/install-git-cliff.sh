#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

# Install git-cliff if not already available.
#
# Usage: source this file, then call install_git_cliff
#   source "$(dirname "$0")/../bootstrap/install-git-cliff.sh"
#   install_git_cliff

# shellcheck source=install-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-common.sh"

# renovate: datasource=github-releases depName=orhun/git-cliff
readonly GIT_CLIFF_VERSION="v2.13.1"

resolve_git_cliff_dist() {
	local os arch dist=""
	os="${OS:-$(uname -s)}"
	arch="${ARCH:-$(uname -m)}"

	case "$os" in
	Linux)
		case "$arch" in
		x86_64 | amd64) dist="git-cliff-${GIT_CLIFF_VERSION#v}-x86_64-unknown-linux-gnu.tar.gz" ;;
		aarch64 | arm64) dist="git-cliff-${GIT_CLIFF_VERSION#v}-aarch64-unknown-linux-gnu.tar.gz" ;;
		esac
		;;
	Darwin)
		case "$arch" in
		x86_64 | amd64) dist="git-cliff-${GIT_CLIFF_VERSION#v}-x86_64-apple-darwin.tar.gz" ;;
		aarch64 | arm64) dist="git-cliff-${GIT_CLIFF_VERSION#v}-aarch64-apple-darwin.tar.gz" ;;
		esac
		;;
	esac

	if [[ -z "$dist" ]]; then
		printf "ERROR: Unsupported git-cliff platform: %s/%s\n" "$os" "$arch" >&2
		return 1
	fi

	printf '%s' "$dist"
}

install_git_cliff() {
	if command -v git-cliff &>/dev/null; then
		ci_print_already_installed "git-cliff" "$(git-cliff --version 2>/dev/null | tr -d '\r')"
		return 0
	fi

	local install_dir dist asset_url archive
	install_dir="$(ci_install_dir git-cliff)"
	mkdir -p "$install_dir"

	dist="$(resolve_git_cliff_dist)" || return 1
	asset_url="https://github.com/orhun/git-cliff/releases/download/${GIT_CLIFF_VERSION}/${dist}"
	archive="$install_dir/${dist}"

	printf "Installing git-cliff (version: %s)...\n" "$GIT_CLIFF_VERSION"
	if ! curl --retry 5 --retry-delay 3 --retry-connrefused -fsSL -o "$archive" "$asset_url"; then
		printf "ERROR: Failed to download git-cliff from %s\n" "$asset_url" >&2
		return 1
	fi

	tar -xzf "$archive" -C "$install_dir" --strip-components=1
	chmod +x "$install_dir/git-cliff"
	ci_prepend_path "$install_dir"

	ci_require_command git-cliff git-cliff || return 1
	ci_print_installed "git-cliff" "$(git-cliff --version 2>/dev/null | tr -d '\r')"
}
