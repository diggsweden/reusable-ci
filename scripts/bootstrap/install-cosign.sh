#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Install cosign (Sigstore signing CLI) if not already available.
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
readonly COSIGN_VERSION="v3.1.1"

install_cosign() {
	if command -v cosign &>/dev/null; then
		ci_print_already_installed "cosign" "$(cosign version 2>/dev/null | grep -o 'GitVersion:[^ ]*' | head -1)"
		return 0
	fi

	local install_dir arch url
	install_dir="$(ci_install_dir cosign)"
	mkdir -p "$install_dir"

	case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*)
		printf "ERROR: unsupported arch for cosign: %s\n" "$(uname -m)" >&2
		return 1
		;;
	esac

	url="https://github.com/sigstore/cosign/releases/download/${COSIGN_VERSION}/cosign-linux-${arch}"

	printf "Installing cosign (version: %s)...\n" "${COSIGN_VERSION}"
	if ! curl --retry 5 --retry-delay 3 --retry-connrefused -sSfL "$url" -o "${install_dir}/cosign"; then
		printf "ERROR: Failed to download cosign %s\n" "${COSIGN_VERSION}" >&2
		return 1
	fi

	chmod 0755 "${install_dir}/cosign"

	ci_prepend_path "$install_dir"
	ci_require_command cosign cosign || return 1
	ci_print_installed "cosign" "$(cosign version 2>/dev/null | grep -o 'GitVersion:[^ ]*' | head -1)"
}
