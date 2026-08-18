#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Builds only the provider-free contract validator, then hands ownership to the
# guarded lifecycle. No contract-supplied cleanup can be trusted before this
# helper exists and validates the complete document.
set -euo pipefail

script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd -P)
go_bin=${GO:-go}
scenario=${1:-}
[[ $# -le 1 ]] || {
	printf 'usage: %s [go-test-run-filter]\n' "$0" >&2
	exit 2
}

state=$(mktemp -d "${TMPDIR:-/tmp}/reusable-ci-live-preflight.XXXXXXXX")
trap 'rm -rf -- "$state"' EXIT

if ! (
	cd -- "$repo_root"
	CGO_ENABLED=0 "$go_bin" build -trimpath -buildvcs=false -o "$state/live-preflight" ./internal/livetest/preflight
	chmod 500 "$state/live-preflight"
); then
	printf 'x live preflight helper build failed before the contract could be validated; no cleanup was armed or executed\n' >&2
	printf 'x the target generation remains producer-owned; validate and invoke its advertised cleanup pair manually\n' >&2
	exit 2
fi

RC_LIVE_PREFLIGHT_BIN="$state/live-preflight" bash "$script_dir/run-live-tests.sh" "$scenario"
