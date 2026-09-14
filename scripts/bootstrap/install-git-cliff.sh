#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Install git-cliff if not already available.
#
# Usage: source this file, then call install_git_cliff
#   source "$(dirname "$0")/../bootstrap/install-git-cliff.sh"
#   install_git_cliff

# shellcheck source=install-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-common.sh"

# renovate: datasource=github-releases depName=orhun/git-cliff
readonly GIT_CLIFF_VERSION="v2.13.1"
readonly GIT_CLIFF_SHA256_LINUX_AMD64="9a1263f24e59a2f508c7b3d3283c9dea94a8bf697f96dbc18cc783cac6284546"
readonly GIT_CLIFF_SHA256_LINUX_ARM64="9619b7f0c584229f8a2331c1905afe88bd938bdc9102926c2073836a42f02455"
readonly GIT_CLIFF_SHA256_DARWIN_AMD64="6e60ae390d375cecb9d8008c49f0e724a8dfe40390b532ef5501e421d2cc8acb"
readonly GIT_CLIFF_SHA256_DARWIN_ARM64="21547ae4a0421164070ab75c2522864ea5565858a011fabc5f583061b20f1226"

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

resolve_git_cliff_sha256() {
	case "$1" in
	git-cliff-2.13.1-x86_64-unknown-linux-gnu.tar.gz) printf '%s' "$GIT_CLIFF_SHA256_LINUX_AMD64" ;;
	git-cliff-2.13.1-aarch64-unknown-linux-gnu.tar.gz) printf '%s' "$GIT_CLIFF_SHA256_LINUX_ARM64" ;;
	git-cliff-2.13.1-x86_64-apple-darwin.tar.gz) printf '%s' "$GIT_CLIFF_SHA256_DARWIN_AMD64" ;;
	git-cliff-2.13.1-aarch64-apple-darwin.tar.gz) printf '%s' "$GIT_CLIFF_SHA256_DARWIN_ARM64" ;;
	*)
		printf 'ERROR: no pinned SHA-256 for git-cliff asset %s\n' "$1" >&2
		return 1
		;;
	esac
}

install_git_cliff() {
	if command -v git-cliff &>/dev/null; then
		ci_print_already_installed "git-cliff" "$(git-cliff --version 2>/dev/null | tr -d '\r')"
		return 0
	fi

	local install_dir dist sha256 asset_url archive
	install_dir="$(ci_install_dir git-cliff)"
	mkdir -p "$install_dir"

	dist="$(resolve_git_cliff_dist)" || return 1
	sha256="$(resolve_git_cliff_sha256 "$dist")" || return 1
	asset_url="https://github.com/orhun/git-cliff/releases/download/${GIT_CLIFF_VERSION}/${dist}"
	archive="$install_dir/${dist}"

	printf "Installing git-cliff (version: %s)...\n" "$GIT_CLIFF_VERSION"
	if ! ci_download_verified "$asset_url" "$archive" "$sha256"; then
		printf "ERROR: Failed to download git-cliff from %s\n" "$asset_url" >&2
		return 1
	fi

	tar -xzf "$archive" -C "$install_dir" --strip-components=1 || return 1
	rm -f "$archive"
	chmod +x "$install_dir/git-cliff"
	ci_prepend_path "$install_dir"

	ci_require_command git-cliff git-cliff || return 1
	ci_print_installed "git-cliff" "$(git-cliff --version 2>/dev/null | tr -d '\r')"
}
