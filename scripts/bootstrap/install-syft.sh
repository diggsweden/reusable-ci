#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

# Install Syft SBOM generator if not already available.
#
# Usage: source this file, then call install_syft
#   source "$(dirname "$0")/../bootstrap/install-syft.sh"
#   install_syft

# shellcheck source=install-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-common.sh"

# renovate: datasource=github-releases depName=anchore/syft
readonly SYFT_VERSION="v1.45.1"

install_syft() {
	if command -v syft &>/dev/null; then
		ci_print_already_installed "Syft" "$(syft version --output json 2>/dev/null | grep -o '"version":"[^"]*"' | cut -d'"' -f4)"
		return 0
	fi

	local install_dir
	install_dir="$(ci_install_dir syft)"
	mkdir -p "$install_dir"

	printf "Installing Syft SBOM generator (version: %s)...\n" "${SYFT_VERSION}"
	if ! curl --retry 5 --retry-delay 3 --retry-connrefused -sSfL https://raw.githubusercontent.com/anchore/syft/main/install.sh | sh -s -- -b "$install_dir" "${SYFT_VERSION}"; then
		printf "ERROR: Failed to install Syft %s\n" "${SYFT_VERSION}" >&2
		return 1
	fi

	ci_prepend_path "$install_dir"
	ci_require_command syft Syft || return 1
	ci_print_installed "Syft" "$(syft version --output json 2>/dev/null | grep -o '"version":"[^"]*"' | cut -d'"' -f4)"
}
