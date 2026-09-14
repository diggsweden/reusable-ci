#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Install the repository-pinned cosign (Sigstore signing CLI).
#
# cosign is required by reusable-ci's Sigstore-keyless and KMS-backed
# signing paths (`release sign --method=sigstore|kms`). The GPG path
# does not use cosign.
#
# Usage: source this file, then call install_cosign
#   source "$(dirname "$0")/../bootstrap/install-cosign.sh"
#   install_cosign

# shellcheck source=install-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-common.sh"

# renovate: datasource=github-releases depName=sigstore/cosign
# cosign 3.x is required: the v3 bundle format is the only output
# layout we support. Adapters pass --bundle / --new-bundle-format.
readonly COSIGN_VERSION="v3.1.3"
readonly COSIGN_SHA256_LINUX_AMD64="4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71"
readonly COSIGN_SHA256_LINUX_ARM64="c5d324e091826b0d7a78eb16fef316450b4eb9aaec045611c08ba06f5e73220a"
readonly COSIGN_SHA256_DARWIN_AMD64="2347488e5d5b25336644024dfeca5601b190e91197a71a917bda44744aff106c"
readonly COSIGN_SHA256_DARWIN_ARM64="5cf948c2f4dfe59687bdd0b8523709067383e03982cc543475c8a7dc70e92a76"

resolve_cosign_asset() {
	local os arch asset=""
	os="${OS:-$(uname -s)}"
	arch="${ARCH:-$(uname -m)}"

	case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	esac
	case "$os/$arch" in
	Linux/amd64 | Linux/arm64) asset="cosign-linux-${arch}" ;;
	Darwin/amd64 | Darwin/arm64) asset="cosign-darwin-${arch}" ;;
	esac

	if [[ -z "$asset" ]]; then
		printf 'ERROR: unsupported cosign platform: %s/%s\n' "$os" "$arch" >&2
		return 1
	fi

	printf '%s' "$asset"
}

resolve_cosign_sha256() {
	case "$1" in
	cosign-linux-amd64) printf '%s' "$COSIGN_SHA256_LINUX_AMD64" ;;
	cosign-linux-arm64) printf '%s' "$COSIGN_SHA256_LINUX_ARM64" ;;
	cosign-darwin-amd64) printf '%s' "$COSIGN_SHA256_DARWIN_AMD64" ;;
	cosign-darwin-arm64) printf '%s' "$COSIGN_SHA256_DARWIN_ARM64" ;;
	*)
		printf 'ERROR: no pinned SHA-256 for cosign asset %s\n' "$1" >&2
		return 1
		;;
	esac
}

install_cosign() {
	local install_dir asset sha256 url download

	install_dir="$(ci_install_dir cosign)"
	if [[ -L "$install_dir" || (-e "$install_dir" && ! -d "$install_dir") ]]; then
		printf 'ERROR: cosign install directory is not a real directory: %s\n' "$install_dir" >&2
		return 1
	fi
	mkdir -p "$install_dir"
	chmod 0700 "$install_dir"

	asset="$(resolve_cosign_asset)" || return 1
	sha256="$(resolve_cosign_sha256 "$asset")" || return 1

	url="https://github.com/sigstore/cosign/releases/download/${COSIGN_VERSION}/${asset}"
	download="$(mktemp "$install_dir/.cosign-download.XXXXXXXX")"

	printf "Installing cosign (version: %s)...\n" "${COSIGN_VERSION}"
	if ! ci_download_verified "$url" "$download" "$sha256"; then
		rm -f "$download"
		printf "ERROR: Failed to download cosign %s\n" "${COSIGN_VERSION}" >&2
		return 1
	fi

	rm -f "${install_dir}/cosign"
	install -m 0755 "$download" "${install_dir}/cosign"
	rm -f "$download"

	ci_prepend_path "$install_dir"
	ci_require_command cosign cosign || return 1
	if ! cosign version 2>/dev/null | grep -Eq "GitVersion:[[:space:]]*${COSIGN_VERSION}([[:space:]]|$)"; then
		printf 'ERROR: installed cosign does not report expected version %s\n' "$COSIGN_VERSION" >&2
		rm -f "${install_dir}/cosign"
		return 1
	fi
	ci_print_installed "cosign" "$(cosign version 2>/dev/null | grep -o 'GitVersion:[^ ]*' | head -1)"
}
