#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Install OpenGrep if not already available.
#
# Usage: source this file, then call install_opengrep
#   source "$(dirname "$0")/../bootstrap/install-opengrep.sh"
#   install_opengrep

# shellcheck source=install-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-common.sh"

# renovate: datasource=github-releases depName=opengrep/opengrep
readonly OPENGREP_VERSION="v1.18.0"
readonly OPENGREP_SHA256_MANYLINUX_AMD64="65390e16db45db1258967578c73eacac6e4ab9cd20074d7dc1aeb695e2696203"
readonly OPENGREP_SHA256_MANYLINUX_ARM64="6253329a98bcd804311a17d4d3dfc947722ca904f0adef64f32bc8f9120755ec"
readonly OPENGREP_SHA256_MUSLLINUX_AMD64="17768ec506bc422ccda3ca3227c2036d77fe5cf0eb6dc0fb010f34ee055c6de6"
readonly OPENGREP_SHA256_MUSLLINUX_ARM64="2d31bdef1791df2bb8402b62b2c5d2edf097de21beb4ef7ce342cc150896d951"
readonly OPENGREP_SHA256_DARWIN_AMD64="2bc2b5c7ce24e9e5171c3ea0b7d86e0d412e8b7d69254dcd72de7791a03566b9"
readonly OPENGREP_SHA256_DARWIN_ARM64="ee851faef1a555e3389cec3ea46bd05f5c8abba2646aceaca70221eebebb96cb"

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

resolve_opengrep_sha256() {
	case "$1" in
	opengrep_manylinux_x86) printf '%s' "$OPENGREP_SHA256_MANYLINUX_AMD64" ;;
	opengrep_manylinux_aarch64) printf '%s' "$OPENGREP_SHA256_MANYLINUX_ARM64" ;;
	opengrep_musllinux_x86) printf '%s' "$OPENGREP_SHA256_MUSLLINUX_AMD64" ;;
	opengrep_musllinux_aarch64) printf '%s' "$OPENGREP_SHA256_MUSLLINUX_ARM64" ;;
	opengrep_osx_x86) printf '%s' "$OPENGREP_SHA256_DARWIN_AMD64" ;;
	opengrep_osx_arm64) printf '%s' "$OPENGREP_SHA256_DARWIN_ARM64" ;;
	*)
		printf 'ERROR: no pinned SHA-256 for OpenGrep asset %s\n' "$1" >&2
		return 1
		;;
	esac
}

install_opengrep() {
	if command -v opengrep &>/dev/null; then
		ci_print_already_installed "OpenGrep" "$(opengrep --version 2>/dev/null | tr -d '\r')"
		return 0
	fi

	local install_dir dist sha256 asset_url download
	install_dir="$(ci_install_dir opengrep)"
	mkdir -p "$install_dir"

	dist="$(resolve_opengrep_dist)" || return 1
	sha256="$(resolve_opengrep_sha256 "$dist")" || return 1
	asset_url="https://github.com/opengrep/opengrep/releases/download/${OPENGREP_VERSION}/${dist}"
	download="$(mktemp "$install_dir/.opengrep-download.XXXXXXXX")"

	printf "Installing OpenGrep (version: %s)...\n" "$OPENGREP_VERSION"
	if ! ci_download_verified "$asset_url" "$download" "$sha256"; then
		rm -f "$download"
		printf "ERROR: Failed to download OpenGrep from %s\n" "$asset_url" >&2
		return 1
	fi

	install -m 0755 "$download" "$install_dir/opengrep"
	rm -f "$download"
	ci_prepend_path "$install_dir"

	ci_require_command opengrep OpenGrep || return 1
	ci_print_installed "OpenGrep" "$(opengrep --version 2>/dev/null | tr -d '\r')"
}
