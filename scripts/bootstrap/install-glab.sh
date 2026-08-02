#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

# Install GitLab CLI (glab) if not already available.
#
# Usage: source this file, then call install_glab
#   source "$(dirname "$0")/../bootstrap/install-glab.sh"
#   install_glab

# shellcheck source=install-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-common.sh"

# renovate: datasource=gitlab-releases depName=gitlab-org/cli
readonly GLAB_VERSION="v1.74.0"

resolve_glab_dist() {
	local os arch dist=""
	os="${OS:-$(uname -s)}"
	arch="${ARCH:-$(uname -m)}"

	case "$os" in
	Linux)
		case "$arch" in
		x86_64 | amd64) dist="glab_${GLAB_VERSION#v}_linux_amd64.tar.gz" ;;
		aarch64 | arm64) dist="glab_${GLAB_VERSION#v}_linux_arm64.tar.gz" ;;
		esac
		;;
	Darwin)
		case "$arch" in
		x86_64 | amd64) dist="glab_${GLAB_VERSION#v}_darwin_amd64.tar.gz" ;;
		aarch64 | arm64) dist="glab_${GLAB_VERSION#v}_darwin_arm64.tar.gz" ;;
		esac
		;;
	esac

	if [[ -z "$dist" ]]; then
		printf "ERROR: Unsupported glab platform: %s/%s\n" "$os" "$arch" >&2
		return 1
	fi

	printf '%s' "$dist"
}

install_glab() {
	if command -v glab &>/dev/null; then
		ci_print_already_installed "glab" "$(glab --version 2>/dev/null | head -1 | tr -d '\r')"
		return 0
	fi

	local install_dir dist asset_url archive
	install_dir="$(ci_install_dir glab)"
	mkdir -p "$install_dir"

	dist="$(resolve_glab_dist)" || return 1
	asset_url="https://gitlab.com/gitlab-org/cli/-/releases/${GLAB_VERSION}/downloads/${dist}"
	archive="$install_dir/${dist}"

	printf "Installing glab (version: %s)...\n" "$GLAB_VERSION"
	if ! curl --retry 5 --retry-delay 3 --retry-connrefused -fsSL -o "$archive" "$asset_url"; then
		printf "ERROR: Failed to download glab from %s\n" "$asset_url" >&2
		return 1
	fi

	tar -xzf "$archive" -C "$install_dir" --strip-components=1
	rm -f "$archive"
	chmod +x "$install_dir/bin/glab" 2>/dev/null || chmod +x "$install_dir/glab"
	# glab tarball variants put binary at bin/glab or directly at root; symlink
	# to the canonical path either way.
	if [[ -x "$install_dir/bin/glab" ]]; then
		mv "$install_dir/bin/glab" "$install_dir/glab"
		rm -rf "${install_dir:?}/bin"
	fi
	ci_prepend_path "$install_dir"

	ci_require_command glab glab || return 1
	ci_print_installed "glab" "$(glab --version 2>/dev/null | head -1 | tr -d '\r')"
}
