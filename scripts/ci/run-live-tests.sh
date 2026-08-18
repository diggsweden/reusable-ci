#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Owns the live run lifecycle: validate and freeze first, capture the exact
# cleanup pair, arm cleanup, then check tools, build, and enter the live tier.
set -euo pipefail

script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd -P)
go_bin=${GO:-go}
scenario=${1:-}
[[ $# -le 1 ]] || {
	printf 'usage: %s [go-test-run-filter]\n' "$0" >&2
	exit 2
}

readonly preflight_source=${RC_LIVE_PREFLIGHT_BIN:?RC_LIVE_PREFLIGHT_BIN must name the prebuilt live preflight helper}
[[ "$preflight_source" == /* && -f "$preflight_source" && ! -L "$preflight_source" && -O "$preflight_source" && -x "$preflight_source" ]] || {
	printf 'RC_LIVE_PREFLIGHT_BIN must be an absolute regular executable\n' >&2
	exit 2
}
# Pin the setup-built inode before accepting the contract. Every later verifier
# invocation opens this descriptor through procfs, not the replaceable pathname.
exec {preflight_fd}<"$preflight_source"
readonly preflight_fd_path="/proc/$$/fd/$preflight_fd"

state=$(mktemp -d "${TMPDIR:-/tmp}/reusable-ci-live.XXXXXXXX")
if ! RC_LIVE_PREFLIGHT_BIN="$preflight_fd_path" RC_LIVE_PREFLIGHT_PINNED=1 \
	bash "$script_dir/validate-live-inputs.sh" --contract "$state/preflight"; then
	rm -rf -- "$state"
	exit 2
fi

readonly frozen_contract="$state/preflight/targets.json"
frozen_contract_facts=$(<"$state/preflight/targets.json-facts")
frozen_ca_facts=$(<"$state/preflight/ca.crt-facts")
cleanup_command=$(<"$state/preflight/cleanup-command")
cleanup_source_command=$(<"$state/preflight/cleanup-source-command")
cleanup_contract_file=$(<"$state/preflight/cleanup-contract-file")
cleanup_command_facts=$(<"$state/preflight/cleanup-command-facts")
cleanup_contract_file_facts=$(<"$state/preflight/cleanup-contract-file-facts")
readonly frozen_contract_facts frozen_ca_facts cleanup_command cleanup_source_command
readonly cleanup_contract_file cleanup_command_facts cleanup_contract_file_facts

cleanup_live_run() {
	local status=$? cleanup_status=0
	trap - EXIT INT TERM

	if "$preflight_fd_path" --run-cleanup \
		--cleanup-command "$cleanup_command" \
		--cleanup-command-facts "$cleanup_command_facts" \
		--cleanup-contract-file "$cleanup_contract_file" \
		--cleanup-contract-file-facts "$cleanup_contract_file_facts"; then
		:
	else
		cleanup_status=$?
		printf 'x frozen cleanup launcher or bound contract_file changed; refusing cleanup execution\n' >&2
	fi
	if ((cleanup_status != 0)); then
		printf 'x automatic live credential cleanup was not proven; manually inspect and invoke the original pair:\n' >&2
		printf '  command: %q\n  contract_file: %q\n' "$cleanup_source_command" "$cleanup_contract_file" >&2
	fi
	rm -rf -- "$state"

	if ((cleanup_status != 0)); then
		status=1
	fi

	exit "$status"
}
trap cleanup_live_run EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# The prebuilt helper has completed; cleanup is armed before host-tool checks,
# product build, and provider work.
RC_LIVE_PREFLIGHT_BIN="$preflight_fd_path" RC_LIVE_PREFLIGHT_PINNED=1 \
	bash "$script_dir/validate-live-inputs.sh" --tools

(
	cd -- "$repo_root"
	CGO_ENABLED=0 "$go_bin" build -trimpath -buildvcs=false -o "$state/reusable-ci" ./cmd/reusable-ci
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 "$go_bin" build -trimpath -buildvcs=false \
		-o "$state/credential-proxy" ./internal/livetest/credentialproxy
	chmod 500 "$state/credential-proxy"
	cd -- "$state"
	sha256sum reusable-ci >reusable-ci.sha256
	sha256sum --check --quiet reusable-ci.sha256
)

export LAB_TARGETS_FILE="$frozen_contract"
export RC_LIVE_BIN="$state/reusable-ci"
export RC_LIVE_CONTRACT_FACTS="$frozen_contract_facts"
export RC_LIVE_CA_FACTS="$frozen_ca_facts"
export RC_LIVE_CREDENTIAL_PROXY_BIN="$state/credential-proxy"

filter=()
[[ -z "$scenario" ]] || filter=(-run "$scenario")
(
	cd -- "$repo_root"
	"$go_bin" test -tags=live -p 1 -count=1 -buildvcs=false -timeout=30m -v "${filter[@]}" ./internal/livetest/...
)
