#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Provider-free preflight for the live-forge conformance tier. Contract mode
# delegates to the same strict Go decoder used by the live process and creates a
# private frozen snapshot. Tool mode is deliberately separate so the caller can
# arm cleanup immediately after complete contract validation and before checking
# host tooling.
set -euo pipefail

readonly preflight_bin=${RC_LIVE_PREFLIGHT_BIN:?RC_LIVE_PREFLIGHT_BIN must name the prebuilt live preflight helper}
if [[ ${RC_LIVE_PREFLIGHT_PINNED:-} == 1 ]]; then
	[[ "$preflight_bin" =~ ^/proc/[0-9]+/fd/[0-9]+$ && -f "$preflight_bin" && -x "$preflight_bin" ]] || {
		printf 'pinned RC_LIVE_PREFLIGHT_BIN must name an executable procfs descriptor\n' >&2
		exit 2
	}
else
	[[ "$preflight_bin" == /* && -f "$preflight_bin" && ! -L "$preflight_bin" && -x "$preflight_bin" ]] || {
		printf 'RC_LIVE_PREFLIGHT_BIN must be an absolute regular executable\n' >&2
		exit 2
	}
fi

validate_contract() {
	local output_dir=${1:?contract mode requires an output directory}
	[[ "$output_dir" == /* ]] || {
		printf 'live preflight output directory must be absolute\n' >&2
		return 2
	}
	"$preflight_bin" --output-dir "$output_dir"
}

validate_tools() {
	local tool
	local -a missing=()
	for tool in cosign syft buildah skopeo sha256sum; do
		command -v "$tool" >/dev/null 2>&1 || missing+=("$tool")
	done
	((${#missing[@]} == 0)) || {
		printf 'live preflight: required tools are not on PATH: %s\n' "${missing[*]}" >&2
		printf 'They are pinned in .mise.toml; install them with: mise install\n' >&2
		return 2
	}
}

case "${1:-}" in
--contract)
	[[ $# -eq 2 ]] || {
		printf 'usage: %s --contract <absolute-new-output-directory>\n' "$0" >&2
		exit 2
	}
	validate_contract "$2"
	;;
--tools)
	[[ $# -eq 1 ]] || {
		printf 'usage: %s --tools\n' "$0" >&2
		exit 2
	}
	validate_tools
	;;
"")
	temporary=$(mktemp -d "${TMPDIR:-/tmp}/reusable-ci-preflight.XXXXXXXX")
	readonly temporary
	trap 'rm -rf -- "$temporary"' EXIT
	validate_contract "$temporary/frozen"
	validate_tools
	;;
*)
	printf 'usage: %s [--contract <absolute-new-output-directory> | --tools]\n' "$0" >&2
	exit 2
	;;
esac
