#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

ci_install_dir() {
	local tool="$1"
	printf '%s/%s-bin' "${CI_TEMP_DIR:-/tmp}" "$tool"
}

ci_verify_sha256() {
	local file="$1"
	local expected="$2"
	local actual

	if command -v sha256sum &>/dev/null; then
		actual="$(sha256sum "$file" | cut -d' ' -f1)"
	elif command -v shasum &>/dev/null; then
		actual="$(shasum -a 256 "$file" | cut -d' ' -f1)"
	else
		printf 'ERROR: neither sha256sum nor shasum is available\n' >&2
		rm -f "$file"
		return 1
	fi

	if [[ "$actual" != "$expected" ]]; then
		printf 'ERROR: SHA-256 mismatch for %s (expected %s, got %s)\n' "$file" "$expected" "$actual" >&2
		rm -f "$file"
		return 1
	fi
}

ci_download_verified() {
	local url="$1"
	local destination="$2"
	local sha256="$3"

	if ! curl --retry 5 --retry-delay 3 --retry-connrefused --proto '=https' --tlsv1.2 -sSfL "$url" -o "$destination"; then
		rm -f "$destination"
		return 1
	fi
	ci_verify_sha256 "$destination" "$sha256"
}

ci_prepend_path() {
	local dir="$1"
	export PATH="${dir}:$PATH"
}

ci_require_command() {
	local cmd="$1"
	local label="$2"
	if ! command -v "$cmd" &>/dev/null; then
		printf 'ERROR: %s binary not found after install\n' "$label" >&2
		return 1
	fi
}

ci_print_already_installed() {
	local label="$1"
	local version="$2"
	printf '%s already installed: %s\n\n' "$label" "$version"
}

ci_print_installed() {
	local label="$1"
	local version="$2"
	printf '%s installed successfully: %s\n\n' "$label" "$version"
}
