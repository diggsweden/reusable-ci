#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Install Syft SBOM generator if not already available.
#
# Usage: source this file, then call install_syft
#   source "$(dirname "$0")/../bootstrap/install-syft.sh"
#   install_syft

# shellcheck source=install-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-common.sh"

# renovate: datasource=github-releases depName=anchore/syft
readonly SYFT_VERSION="v1.45.1"
# Archive hashes are independently pinned here rather than downloaded beside
# the archive. Source: the upstream v1.45.1 release checksums asset.
readonly SYFT_SHA256_LINUX_AMD64="20c84195e24927f50a3b2269946be51f4c4abc9d2f145fee7388b4199149f716"
readonly SYFT_SHA256_LINUX_ARM64="7df9f45cba1f6358ecfc7fac349d43b4605137001f9646b41267abe15a7c6cd7"
readonly SYFT_SHA256_DARWIN_AMD64="abe6e73b819f433b69ece755dc180a19c7694896062bf806f89d0e3ca5db710a"
readonly SYFT_SHA256_DARWIN_ARM64="2f79ccbba6236636125d1ece60a6dc71d4e4f91b9f580cc2afbbafc763ff353d"

install_syft() {
	if command -v syft &>/dev/null; then
		ci_print_already_installed "Syft" "$(syft version --output json 2>/dev/null | grep -o '"version":"[^"]*"' | cut -d'"' -f4)"
		return 0
	fi

	local install_dir os arch asset sha256 archive version
	install_dir="$(ci_install_dir syft)"
	mkdir -p "$install_dir"
	version="${SYFT_VERSION#v}"

	case "$(uname -s):$(uname -m)" in
	Linux:x86_64)
		os=linux
		arch=amd64
		sha256="$SYFT_SHA256_LINUX_AMD64"
		;;
	Linux:aarch64 | Linux:arm64)
		os=linux
		arch=arm64
		sha256="$SYFT_SHA256_LINUX_ARM64"
		;;
	Darwin:x86_64)
		os=darwin
		arch=amd64
		sha256="$SYFT_SHA256_DARWIN_AMD64"
		;;
	Darwin:arm64)
		os=darwin
		arch=arm64
		sha256="$SYFT_SHA256_DARWIN_ARM64"
		;;
	*)
		printf 'ERROR: unsupported platform for Syft: %s/%s\n' "$(uname -s)" "$(uname -m)" >&2
		return 1
		;;
	esac
	asset="syft_${version}_${os}_${arch}.tar.gz"
	archive="$install_dir/$asset"

	printf "Installing Syft SBOM generator (version: %s)...\n" "${SYFT_VERSION}"
	if ! ci_download_verified "https://github.com/anchore/syft/releases/download/${SYFT_VERSION}/${asset}" "$archive" "$sha256" ||
		! tar -xzf "$archive" -C "$install_dir" syft; then
		rm -f "$archive" "$install_dir/syft"
		printf "ERROR: Failed to install Syft %s\n" "${SYFT_VERSION}" >&2
		return 1
	fi
	rm -f "$archive"

	ci_prepend_path "$install_dir"
	ci_require_command syft Syft || return 1
	ci_print_installed "Syft" "$(syft version --output json 2>/dev/null | grep -o '"version":"[^"]*"' | cut -d'"' -f4)"
}
