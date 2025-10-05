#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Install Trivy vulnerability scanner if not already available.
#
# Usage: source this file, then call install_trivy
#   source "$(dirname "$0")/../bootstrap/install-trivy.sh"
#   install_trivy

# shellcheck source=install-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-common.sh"

# renovate: datasource=github-releases depName=aquasecurity/trivy
readonly TRIVY_VERSION="v0.69.3"
# Archive hashes are independently pinned here rather than downloaded beside
# the archive. Source: the upstream v0.69.3 release checksums asset.
readonly TRIVY_SHA256_LINUX_AMD64="1816b632dfe529869c740c0913e36bd1629cb7688bd5634f4a858c1d57c88b75"
readonly TRIVY_SHA256_LINUX_ARM64="7e3924a974e912e57b4a99f65ece7931f8079584dae12eb7845024f97087bdfd"
readonly TRIVY_SHA256_DARWIN_AMD64="fec4a9f7569b624dd9d044fca019e5da69e032700edbb1d7318972c448ec2f4e"
readonly TRIVY_SHA256_DARWIN_ARM64="a2f2179afd4f8bb265ca3c7aefb56a666bc4a9a411663bc0f22c3549fbc643a5"

install_trivy() {
	if command -v trivy &>/dev/null; then
		ci_print_already_installed "Trivy" "$(trivy --version 2>/dev/null | head -1)"
		return 0
	fi

	local install_dir os arch asset sha256 archive version
	install_dir="$(ci_install_dir trivy)"
	mkdir -p "$install_dir"
	version="${TRIVY_VERSION#v}"

	case "$(uname -s):$(uname -m)" in
	Linux:x86_64)
		os=Linux
		arch=64bit
		sha256="$TRIVY_SHA256_LINUX_AMD64"
		;;
	Linux:aarch64 | Linux:arm64)
		os=Linux
		arch=ARM64
		sha256="$TRIVY_SHA256_LINUX_ARM64"
		;;
	Darwin:x86_64)
		os=macOS
		arch=64bit
		sha256="$TRIVY_SHA256_DARWIN_AMD64"
		;;
	Darwin:arm64)
		os=macOS
		arch=ARM64
		sha256="$TRIVY_SHA256_DARWIN_ARM64"
		;;
	*)
		printf 'ERROR: unsupported platform for Trivy: %s/%s\n' "$(uname -s)" "$(uname -m)" >&2
		return 1
		;;
	esac
	asset="trivy_${version}_${os}-${arch}.tar.gz"
	archive="$install_dir/$asset"

	printf "Installing Trivy vulnerability scanner (version: %s)...\n" "${TRIVY_VERSION}"
	if ! ci_download_verified "https://github.com/aquasecurity/trivy/releases/download/${TRIVY_VERSION}/${asset}" "$archive" "$sha256" ||
		! tar -xzf "$archive" -C "$install_dir" trivy; then
		rm -f "$archive" "$install_dir/trivy"
		printf "ERROR: Failed to install Trivy %s\n" "${TRIVY_VERSION}" >&2
		return 1
	fi
	rm -f "$archive"

	ci_prepend_path "$install_dir"
	ci_require_command trivy Trivy || return 1
	ci_print_installed "Trivy" "$(trivy --version 2>/dev/null | head -1)"
}
