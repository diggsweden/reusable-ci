#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Install the publiccode-parser-go CLI if not already available.
#
# Usage: source this file, then call install_publiccode_parser
#   source "$(dirname "$0")/../bootstrap/install-publiccode-parser.sh"
#   install_publiccode_parser

# shellcheck source=install-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-common.sh"

# renovate: datasource=github-releases depName=italia/publiccode-parser-go
readonly PUBLICCODE_PARSER_VERSION="v5.3.1"
readonly PUBLICCODE_PARSER_SHA256_LINUX_AMD64="ed5b75ebe3fc3f6c29f925d8acc132e381769690cada3b40680406b9ec64fedf"
readonly PUBLICCODE_PARSER_SHA256_LINUX_ARM64="556b0fe4d8a6e9d2f638baa174212930ae463da6f5911543a55f29018bea78ae"
readonly PUBLICCODE_PARSER_SHA256_DARWIN_AMD64="b283c916c5fface7434a530f39eadc452ea65bb9c3bc12e1446a06714905a453"
readonly PUBLICCODE_PARSER_SHA256_DARWIN_ARM64="ca7a332ddda144e7f7f5569fe207799298db5fc1fd9129cff312a9ffe658543f"

resolve_publiccode_parser_dist() {
	local os arch dist=""
	os="${OS:-$(uname -s)}"
	arch="${ARCH:-$(uname -m)}"

	case "$os" in
	Linux)
		case "$arch" in
		x86_64 | amd64) dist="publiccode-parser-go_Linux_x86_64.tar.gz" ;;
		aarch64 | arm64) dist="publiccode-parser-go_Linux_arm64.tar.gz" ;;
		esac
		;;
	Darwin)
		case "$arch" in
		x86_64 | amd64) dist="publiccode-parser-go_Darwin_x86_64.tar.gz" ;;
		aarch64 | arm64) dist="publiccode-parser-go_Darwin_arm64.tar.gz" ;;
		esac
		;;
	esac

	if [[ -z "$dist" ]]; then
		printf "ERROR: Unsupported publiccode-parser platform: %s/%s\n" "$os" "$arch" >&2
		return 1
	fi

	printf '%s' "$dist"
}

resolve_publiccode_parser_sha256() {
	case "$1" in
	publiccode-parser-go_Linux_x86_64.tar.gz) printf '%s' "$PUBLICCODE_PARSER_SHA256_LINUX_AMD64" ;;
	publiccode-parser-go_Linux_arm64.tar.gz) printf '%s' "$PUBLICCODE_PARSER_SHA256_LINUX_ARM64" ;;
	publiccode-parser-go_Darwin_x86_64.tar.gz) printf '%s' "$PUBLICCODE_PARSER_SHA256_DARWIN_AMD64" ;;
	publiccode-parser-go_Darwin_arm64.tar.gz) printf '%s' "$PUBLICCODE_PARSER_SHA256_DARWIN_ARM64" ;;
	*)
		printf 'ERROR: no pinned SHA-256 for publiccode-parser asset %s\n' "$1" >&2
		return 1
		;;
	esac
}

install_publiccode_parser() {
	if command -v publiccode-parser &>/dev/null; then
		ci_print_already_installed "publiccode-parser" "$(publiccode-parser --version 2>/dev/null | head -1 | tr -d '\r')"
		return 0
	fi

	local install_dir dist sha256 asset_url archive
	install_dir="$(ci_install_dir publiccode-parser)"
	mkdir -p "$install_dir"

	dist="$(resolve_publiccode_parser_dist)" || return 1
	sha256="$(resolve_publiccode_parser_sha256 "$dist")" || return 1
	asset_url="https://github.com/italia/publiccode-parser-go/releases/download/${PUBLICCODE_PARSER_VERSION}/${dist}"
	archive="$install_dir/${dist}"

	printf "Installing publiccode-parser (version: %s)...\n" "$PUBLICCODE_PARSER_VERSION"
	if ! ci_download_verified "$asset_url" "$archive" "$sha256"; then
		printf "ERROR: Failed to download publiccode-parser from %s\n" "$asset_url" >&2
		return 1
	fi

	tar -xzf "$archive" -C "$install_dir"
	rm -f "$archive"
	chmod +x "$install_dir/publiccode-parser"
	ci_prepend_path "$install_dir"

	ci_require_command publiccode-parser publiccode-parser || return 1
	ci_print_installed "publiccode-parser" "$(publiccode-parser --version 2>/dev/null | head -1 | tr -d '\r')"
}
