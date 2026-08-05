#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

# Install reusable-ci on plain/macOS runners where the runtime container is not
# available.
#
# Resolution:
#   "local" / "."        → go install ./cmd/reusable-ci (working copy)
#   vX.Y.Z[(-suffix)]    → download release tarball + verify SHA-256
#   anything else (SHAs, branches) → go install github.com/diggsweden/reusable-ci/v3/cmd/reusable-ci@<ref>
#
# Release refs use only the authenticated release-asset path and fail closed on
# any download or verification error. Set REUSABLE_CI_USE_GO_INSTALL=1 only
# when explicitly choosing source installation instead.

# shellcheck source=install-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-common.sh"

# Repository owner/name and release URL base are env-overridable so tests can
# point at a fake server. Defaults match the production GitHub Releases path.
: "${REUSABLE_CI_RELEASE_URL_BASE:=https://github.com/diggsweden/reusable-ci/releases/download}"
: "${REUSABLE_CI_MODULE_PATH:=github.com/diggsweden/reusable-ci/v3/cmd/reusable-ci}"

# resolve_reusable_ci_os prints linux|darwin, or fails on unsupported hosts.
resolve_reusable_ci_os() {
	local os
	os="$(uname -s)"
	case "$os" in
	Linux) printf 'linux' ;;
	Darwin) printf 'darwin' ;;
	*)
		printf 'ERROR: Unsupported reusable-ci OS: %s\n' "$os" >&2
		return 1
		;;
	esac
}

# resolve_reusable_ci_arch prints amd64|arm64, or fails on unsupported hosts.
resolve_reusable_ci_arch() {
	local arch
	arch="$(uname -m)"
	case "$arch" in
	x86_64 | amd64) printf 'amd64' ;;
	aarch64 | arm64) printf 'arm64' ;;
	*)
		printf 'ERROR: Unsupported reusable-ci arch: %s\n' "$arch" >&2
		return 1
		;;
	esac
}

# resolve_reusable_ci_dist prints the release-asset filename for (ref, os, arch).
# Strips the leading "v" from the version because GoReleaser does the same.
resolve_reusable_ci_dist() {
	local ref="$1" os="$2" arch="$3"
	local version="${ref#v}"
	printf 'reusable-ci_%s_%s_%s.tar.gz' "$version" "$os" "$arch"
}

# verify_reusable_ci_binary_pin enforces an exact SHA-256 pin on the
# INSTALLED BINARY when REUSABLE_CI_BINARY_SHA256 is set (the same pin
# variable forgejo-ci's signer toolchain asserts). This is defense in
# depth on top of cosign + checksums.txt and also covers explicit source
# installs. Fail-closed: on
# mismatch the binary is removed and the install errors.
verify_reusable_ci_binary_pin() {
	local binary="$1"
	local pin="${REUSABLE_CI_BINARY_SHA256:-}"
	[[ -z "$pin" ]] && return 0
	local got
	if command -v sha256sum &>/dev/null; then
		got="$(sha256sum "$binary" | awk '{print $1}')"
	elif command -v shasum &>/dev/null; then
		got="$(shasum -a 256 "$binary" | awk '{print $1}')"
	else
		printf 'ERROR: REUSABLE_CI_BINARY_SHA256 set but no sha256 helper available\n' >&2
		rm -f "$binary"
		return 1
	fi
	if [[ "$got" != "$pin" ]]; then
		printf 'ERROR: installed reusable-ci does not match REUSABLE_CI_BINARY_SHA256:\n  got:  %s\n  want: %s\n' "$got" "$pin" >&2
		rm -f "$binary"
		return 1
	fi
	printf 'Binary pin: reusable-ci sha256 %s verified\n' "$pin"
}

# is_reusable_ci_release_ref reports whether ref looks like a published semver
# tag (vN.N.N with optional -suffix). Branch names and SHAs fall through.
is_reusable_ci_release_ref() {
	local ref="$1"
	[[ "$ref" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9._-]+)?$ ]]
}

# verify_reusable_ci_sha256 reads a goreleaser-style checksums.txt
# ("<hex>  <filename>") and verifies <archive> matches.
verify_reusable_ci_sha256() {
	local archive="$1" checksums="$2"
	local want got name
	name="$(basename "$archive")"
	want="$(awk -v n="$name" '$2 == n {print $1; exit}' "$checksums")"
	if [[ -z "$want" ]]; then
		printf 'ERROR: %s not in checksums.txt\n' "$name" >&2
		return 1
	fi
	if command -v sha256sum &>/dev/null; then
		got="$(sha256sum "$archive" | awk '{print $1}')"
	elif command -v shasum &>/dev/null; then
		got="$(shasum -a 256 "$archive" | awk '{print $1}')"
	else
		printf 'ERROR: neither sha256sum nor shasum available for SHA-256 verification\n' >&2
		return 1
	fi
	if [[ "$got" != "$want" ]]; then
		printf 'ERROR: %s SHA-256 mismatch:\n  got:  %s\n  want: %s\n' "$name" "$got" "$want" >&2
		return 1
	fi
}

# verify_reusable_ci_cosign verifies the Sigstore v3 bundle alongside
# the checksums file. FAIL-CLOSED: a missing cosign binary or a missing
# signature bundle is an error.
#
# Identity is pinned to the diggsweden/reusable-ci workflow that produced the
# signature. Two trust domains, selected by ref (see _reusable_ci_cosign_identity):
#   vN.N.N      -> release-binary.yml on a release tag (production)
#   *-pre      -> build-cli.yml on a development branch (rolling pre-release channel)
# A signature from any other workflow/ref fails the check even if cosign accepts
# the bundle. REUSABLE_CI_COSIGN_IDENTITY overrides both.
_reusable_ci_cosign_identity() {
	local ref="$1"
	if [[ -n "${REUSABLE_CI_COSIGN_IDENTITY:-}" ]]; then
		printf '%s' "$REUSABLE_CI_COSIGN_IDENTITY"
	elif [[ "$ref" == *-pre ]]; then
		printf '%s' '^https://github.com/diggsweden/reusable-ci/\.github/workflows/build-cli\.yml@refs/heads/(main|feat/refactor-go)$'
	else
		printf '%s' '^https://github.com/diggsweden/reusable-ci/\.github/workflows/release-binary\.yml@refs/heads/main$'
	fi
}

verify_reusable_ci_cosign() {
	local checksums="$1" bundle="$2" ref="${3:-}"

	if ! command -v cosign &>/dev/null; then
		printf 'ERROR: cosign not on PATH; install cosign with scripts/bootstrap/install-cosign.sh\n' >&2
		return 1
	fi

	if [[ ! -f "$bundle" ]]; then
		printf 'ERROR: %s missing; the release carries no signature bundle\n' "$bundle" >&2
		return 1
	fi

	local identity issuer
	identity="$(_reusable_ci_cosign_identity "$ref")"
	issuer="${REUSABLE_CI_COSIGN_ISSUER:-https://token.actions.githubusercontent.com}"

	if ! cosign verify-blob \
		--bundle "$bundle" \
		--new-bundle-format \
		--certificate-identity-regexp "$identity" \
		--certificate-oidc-issuer "$issuer" \
		"$checksums" >/dev/null 2>&1; then
		printf 'ERROR: cosign verification of %s failed against identity %s\n' "$(basename "$checksums")" "$identity" >&2
		return 1
	fi

	printf 'Sigstore: %s verified (identity-regex %s)\n' "$(basename "$checksums")" "$identity"
}

# install_reusable_ci_release downloads the release asset for ref and extracts
# the `reusable-ci` binary into install_dir. Returns non-zero on any failure;
# callers must propagate the failure for release refs.
install_reusable_ci_release() {
	local ref="$1" install_dir="$2"
	if ! command -v curl &>/dev/null; then
		return 1
	fi
	local os arch dist tmp
	os="$(resolve_reusable_ci_os)" || return 1
	arch="$(resolve_reusable_ci_arch)" || return 1
	dist="$(resolve_reusable_ci_dist "$ref" "$os" "$arch")"
	tmp="$(mktemp -d)"
	trap 'rm -rf "$tmp"' RETURN

	# Retry transient network / 5xx failures (e.g. GitHub Releases returning a
	# 504 gateway timeout). `--retry` already covers HTTP 408/429/5xx + timeouts;
	# `--retry-connrefused` adds connection-refused. Both are old/portable flags.
	local -a curl_retry=(--retry 5 --retry-delay 3 --retry-connrefused)

	local asset_url="${REUSABLE_CI_RELEASE_URL_BASE}/${ref}/${dist}"
	local sums_url="${REUSABLE_CI_RELEASE_URL_BASE}/${ref}/checksums.txt"
	local bundle_url="${REUSABLE_CI_RELEASE_URL_BASE}/${ref}/checksums.txt.bundle"
	printf 'Downloading reusable-ci %s (%s/%s)...\n' "$ref" "$os" "$arch"
	if ! curl "${curl_retry[@]}" -sSfL -o "$tmp/$dist" "$asset_url"; then
		printf 'WARN: failed to download %s\n' "$asset_url" >&2
		return 1
	fi
	if ! curl "${curl_retry[@]}" -sSfL -o "$tmp/checksums.txt" "$sums_url"; then
		printf 'WARN: failed to download %s\n' "$sums_url" >&2
		return 1
	fi
	if ! curl "${curl_retry[@]}" -sSfL -o "$tmp/checksums.txt.bundle" "$bundle_url"; then
		printf 'ERROR: failed to download %s\n' "$bundle_url" >&2
		return 1
	fi
	if ! verify_reusable_ci_cosign "$tmp/checksums.txt" "$tmp/checksums.txt.bundle" "$ref"; then
		return 1
	fi
	if ! verify_reusable_ci_sha256 "$tmp/$dist" "$tmp/checksums.txt"; then
		return 1
	fi
	mkdir -p "$install_dir"
	if ! tar -xzf "$tmp/$dist" -C "$install_dir" reusable-ci; then
		printf 'ERROR: failed to extract reusable-ci from %s\n' "$dist" >&2
		return 1
	fi
	chmod +x "$install_dir/reusable-ci"
}

install_reusable_ci_go_install() {
	local ref="$1" install_dir="$2"
	if ! command -v go &>/dev/null; then
		printf 'ERROR: go binary not found for requested source installation\n' >&2
		return 1
	fi
	mkdir -p "$install_dir"
	if [[ "$ref" == "local" || "$ref" == "." ]]; then
		GOBIN="$install_dir" go install ./cmd/reusable-ci
	else
		GOBIN="$install_dir" go install "${REUSABLE_CI_MODULE_PATH}@${ref}"
	fi
}

install_reusable_ci() {
	local ref="${1:-${REUSABLE_CI_BINARY_REF:-v3.0.0}}"
	local install_dir="${REUSABLE_CI_INSTALL_DIR:-${RUNNER_TEMP:-${CI_TEMP_DIR:-/tmp}}/bin}"

	if [[ -z "$ref" ]]; then
		printf 'ERROR: reusable-ci ref is required\n' >&2
		return 1
	fi

	printf 'Installing reusable-ci (%s) into %s...\n' "$ref" "$install_dir"
	if [[ "$ref" == "local" || "$ref" == "." ]]; then
		install_reusable_ci_go_install "$ref" "$install_dir" || return 1
	elif [[ "${REUSABLE_CI_USE_GO_INSTALL:-0}" != "1" ]] && is_reusable_ci_release_ref "$ref"; then
		install_reusable_ci_release "$ref" "$install_dir" || return 1
	else
		install_reusable_ci_go_install "$ref" "$install_dir" || return 1
	fi

	verify_reusable_ci_binary_pin "$install_dir/reusable-ci" || return 1

	ci_prepend_path "$install_dir"
	if [[ -n "${GITHUB_PATH:-}" ]]; then
		printf '%s\n' "$install_dir" >>"$GITHUB_PATH"
	fi
	ci_require_command reusable-ci reusable-ci || return 1
	ci_print_installed "reusable-ci" "$(reusable-ci --version 2>/dev/null || printf 'version unavailable')"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	install_reusable_ci "$@"
fi
