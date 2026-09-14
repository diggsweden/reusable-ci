#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Owns the live run lifecycle: validate and freeze first, capture the exact
# cleanup pair, arm cleanup, then check tools, build, and enter the live tier.
set -euo pipefail
umask 077

script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd -P)
go_bin=${GO:-go}
profile=${1:-}
scenario=
expected_road=
case "$profile" in
full)
	[[ $# -eq 2 ]] || {
		printf 'usage: %s full <compose|k3s>\n       %s focused <go-test-run-filter>\n' "$0" "$0" >&2
		exit 2
	}
	expected_road=$2
	[[ "$expected_road" == compose || "$expected_road" == k3s ]] || {
		printf 'full live profile road must be compose or k3s, got %q\n' "$expected_road" >&2
		exit 2
	}
	;;
focused)
	[[ $# -eq 2 && -n "$2" ]] || {
		printf 'usage: %s focused <go-test-run-filter>\n' "$0" >&2
		exit 2
	}
	scenario=$2
	;;
*)
	printf 'live profile must be explicit: full <compose|k3s> or focused <go-test-run-filter>\n' >&2
	exit 2
	;;
esac
export RC_LIVE_PROFILE="$profile"
export RC_LIVE_EXPECT_ROAD="$expected_road"

readonly evidence_retention_days=30
readonly max_log_bytes=10485760
readonly max_source_manifest_bytes=2097152
readonly max_binary_manifest_bytes=65536
readonly max_metadata_bytes=65536

# Redacts credentials by shape, because the evidence wrapper runs before the
# contract is read and cannot know the values: forge token prefixes, credential
# headers (a cookie to the end of the line), URL userinfo, credential flags
# given as a separate argument, and credential-named keys in env, JSON and
# auth-file form.
sanitize_live_log() {
	sed -E \
		-e 's/(gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,}|glpat-[A-Za-z0-9_-]{20,})/[REDACTED]/g' \
		-e 's/((Authorization|Proxy-Authorization|PRIVATE-TOKEN|JOB-TOKEN|X-Registry-Auth):[[:space:]]*((Basic|Bearer|token)[[:space:]]*)?)[^[:space:]"]+/\1[REDACTED]/Ig' \
		-e 's/((Set-)?Cookie:[[:space:]]*).*/\1[REDACTED]/Ig' \
		-e 's#(://)[^/@[:space:]]+@#\1[REDACTED]@#g' \
		-e 's/(--?(password|passwd|token|secret)[[:space:]]+)[^[:space:]]+/\1[REDACTED]/Ig' \
		-e "s/((\\bauth|token|password|passwd|secret|client_secret)[\"']?[[:space:]]*[:=][[:space:]]*[\"']?)[^,\"'[:space:]}]+/\\1[REDACTED]/Ig"
}

setup_live_evidence() {
	local evidence_root run_id source_commit state_home

	if [[ -n ${RC_LIVE_EVIDENCE_ROOT:-} ]]; then
		evidence_root=$RC_LIVE_EVIDENCE_ROOT
	else
		state_home=${XDG_STATE_HOME:-${HOME:+$HOME/.local/state}}
		[[ -n "$state_home" ]] || {
			printf 'XDG_STATE_HOME or HOME is required for durable live evidence\n' >&2
			return 2
		}
		evidence_root="$state_home/reusable-ci/live-runs"
	fi
	[[ "$evidence_root" == /* ]] || {
		printf 'RC_LIVE_EVIDENCE_ROOT must be absolute, got %q\n' "$evidence_root" >&2
		return 2
	}
	[[ ! -e "$evidence_root" || (-d "$evidence_root" && ! -L "$evidence_root" && -O "$evidence_root") ]] || {
		printf 'live evidence root must be an owner-controlled, non-symlink directory: %q\n' "$evidence_root" >&2
		return 2
	}
	install -d -m 0700 -- "$evidence_root"
	chmod 0700 -- "$evidence_root"
	find "$evidence_root" -mindepth 1 -maxdepth 1 -type d -name '20??????T??????Z-[0-9]*' \
		-mtime "+$evidence_retention_days" -exec rm -rf -- {} +

	run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
	RC_LIVE_EVIDENCE_DIR="$evidence_root/$run_id"
	export RC_LIVE_EVIDENCE_DIR
	install -d -m 0700 -- "$RC_LIVE_EVIDENCE_DIR"
	: >"$RC_LIVE_EVIDENCE_DIR/live-run.log"
	: >"$RC_LIVE_EVIDENCE_DIR/source-files.sha256"
	: >"$RC_LIVE_EVIDENCE_DIR/built-binaries.sha256"
	source_commit=$(git -C "$repo_root" rev-parse --verify HEAD 2>/dev/null || printf 'unavailable')
	{
		printf 'run_id=%s\n' "$run_id"
		printf 'started_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
		printf 'source_commit=%s\n' "$source_commit"
		printf 'profile=%s\n' "$profile"
		printf 'expected_road=%s\n' "$expected_road"
		printf 'scenario=%q\n' "$scenario"
		printf 'log_limit_bytes=%d\n' "$max_log_bytes"
		printf 'source_manifest_limit_bytes=%d\n' "$max_source_manifest_bytes"
		printf 'binary_manifest_limit_bytes=%d\n' "$max_binary_manifest_bytes"
		printf 'retention_days=%d\n' "$evidence_retention_days"
	} >"$RC_LIVE_EVIDENCE_DIR/metadata.txt"
	chmod 0600 -- "$RC_LIVE_EVIDENCE_DIR"/*
}

seal_live_evidence() {
	local evidence_dir=$1 log_size metadata_size total_size truncated

	rm -f -- "$evidence_dir"/*.tmp
	log_size=$(wc -c <"$evidence_dir/live-run.log")
	if ((log_size > max_log_bytes)); then
		truncated="$evidence_dir/live-run.log.tmp"
		{
			printf '[earlier output removed to enforce the %d-byte evidence limit]\n' "$max_log_bytes"
			tail -c $((max_log_bytes - 256)) "$evidence_dir/live-run.log"
		} >"$truncated"
		mv -- "$truncated" "$evidence_dir/live-run.log"
	fi
	[[ $(wc -c <"$evidence_dir/source-files.sha256") -le $max_source_manifest_bytes ]] || {
		printf 'live source manifest exceeds %d bytes\n' "$max_source_manifest_bytes" >&2
		return 1
	}
	[[ $(wc -c <"$evidence_dir/built-binaries.sha256") -le $max_binary_manifest_bytes ]] || {
		printf 'live binary manifest exceeds %d bytes\n' "$max_binary_manifest_bytes" >&2
		return 1
	}
	metadata_size=$(wc -c <"$evidence_dir/metadata.txt")
	((metadata_size <= max_metadata_bytes)) || {
		printf 'live evidence metadata exceeds %d bytes\n' "$max_metadata_bytes" >&2
		return 1
	}
	(
		cd -- "$evidence_dir"
		sha256sum -- metadata.txt source-files.sha256 built-binaries.sha256 live-run.log >SHA256SUMS
		sha256sum --check --quiet SHA256SUMS
		chmod 0400 -- metadata.txt source-files.sha256 built-binaries.sha256 live-run.log SHA256SUMS
	)
	total_size=$(du -sb -- "$evidence_dir" | cut -f1)
	((total_size <= max_log_bytes + max_source_manifest_bytes + max_binary_manifest_bytes + max_metadata_bytes + 65536)) || {
		printf 'live evidence bundle exceeds its aggregate size limit\n' >&2
		return 1
	}
}

if [[ ${RC_LIVE_EVIDENCE_CAPTURED:-} != 1 ]]; then
	setup_live_evidence
	outer_signal_status=
	trap 'outer_signal_status=130' INT
	trap 'outer_signal_status=143' TERM
	set +e
	# The capture ignores INT and TERM so it outlives the run it records. A
	# terminal or a cancelled job signals the whole process group; if sed and tee
	# died with it, the run's report of its own terminated child would hit a
	# broken pipe and SIGPIPE would kill it before the credential cleanup trap
	# ran. The capture ends when the run closes its output.
	RC_LIVE_EVIDENCE_CAPTURED=1 bash "$script_dir/run-live-tests.sh" "$@" 2>&1 |
		(
			trap '' INT TERM
			sanitize_live_log
		) | (
		trap '' INT TERM
		exec tee "$RC_LIVE_EVIDENCE_DIR/live-run.log"
	)
	pipeline_status=("${PIPESTATUS[@]}")
	set -e
	trap - INT TERM
	status=${pipeline_status[0]}
	for pipeline_part in "${pipeline_status[@]:1}"; do
		((pipeline_part == 0)) || status=1
	done
	[[ -z "$outer_signal_status" ]] || status=$outer_signal_status
	{
		printf 'completed_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
		printf 'exit_status=%d\n' "$status"
	} >>"$RC_LIVE_EVIDENCE_DIR/metadata.txt"
	if ! seal_live_evidence "$RC_LIVE_EVIDENCE_DIR"; then
		status=1
	fi
	printf 'Live evidence: %s (retained for %d days)\n' "$RC_LIVE_EVIDENCE_DIR" "$evidence_retention_days"
	exit "$status"
fi

readonly evidence_dir=${RC_LIVE_EVIDENCE_DIR:?RC_LIVE_EVIDENCE_DIR must name the prepared durable evidence directory}
[[ "$evidence_dir" == /* && -d "$evidence_dir" && ! -L "$evidence_dir" && -O "$evidence_dir" ]] || {
	printf 'RC_LIVE_EVIDENCE_DIR must be an absolute owner-controlled, non-symlink directory\n' >&2
	exit 2
}

write_source_manifest() {
	local digest file manifest_tmp source_list target
	manifest_tmp="$evidence_dir/source-files.sha256.tmp"
	source_list="$evidence_dir/source-files.list.tmp"
	if ! (cd -- "$repo_root" && git ls-files --cached --others --exclude-standard -z) >"$source_list"; then
		rm -f -- "$source_list"
		printf 'could not enumerate the live-run source tree\n' >&2
		return 1
	fi
	(
		cd -- "$repo_root"
		while IFS= read -r -d '' file; do
			if [[ -L "$file" ]]; then
				target=$(readlink -- "$file")
				digest=$(printf 'symlink:%s' "$target" | sha256sum)
				printf '%s  symlink %q\n' "${digest%% *}" "$file"
			elif [[ -f "$file" ]]; then
				digest=$(sha256sum -- "$file")
				printf '%s  file %q\n' "${digest%% *}" "$file"
			else
				printf '%064d  missing %q\n' 0 "$file"
			fi
		done <"$source_list"
	) >"$manifest_tmp"
	rm -f -- "$source_list"
	[[ $(wc -c <"$manifest_tmp") -le $max_source_manifest_bytes ]] || {
		rm -f -- "$manifest_tmp"
		printf 'live source manifest exceeds %d bytes\n' "$max_source_manifest_bytes" >&2
		return 1
	}
	mv -- "$manifest_tmp" "$evidence_dir/source-files.sha256"
	chmod 0600 -- "$evidence_dir/source-files.sha256"
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
		if ((cleanup_status == 3)); then
			printf 'x frozen cleanup launcher failed or exceeded its time bound\n' >&2
		else
			printf 'x frozen cleanup launcher or bound contract_file changed; refusing cleanup execution\n' >&2
		fi
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

write_source_manifest

(
	cd -- "$repo_root"
	CGO_ENABLED=0 "$go_bin" build -trimpath -buildvcs=false -o "$state/reusable-ci-host" ./cmd/reusable-ci
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 "$go_bin" build -trimpath -buildvcs=false \
		-o "$state/reusable-ci-runner" ./cmd/reusable-ci
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 "$go_bin" build -trimpath -buildvcs=false \
		-o "$state/credential-proxy" ./internal/livetest/credentialproxy
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 "$go_bin" build -trimpath -buildvcs=false \
		-o "$state/probe-json" ./internal/livetest/probejson
	chmod 500 "$state/reusable-ci-host" "$state/reusable-ci-runner" "$state/credential-proxy" "$state/probe-json"
	cd -- "$state"
	sha256sum reusable-ci-host reusable-ci-runner credential-proxy probe-json >built-artifacts.sha256
	sha256sum --check --quiet built-artifacts.sha256
	./reusable-ci-host --version >/dev/null
)
cp -- "$state/built-artifacts.sha256" "$evidence_dir/built-binaries.sha256"
chmod 0600 -- "$evidence_dir/built-binaries.sha256"

export LAB_TARGETS_FILE="$frozen_contract"
export RC_LIVE_BIN="$state/reusable-ci-host"
export RC_LIVE_RUNNER_BIN="$state/reusable-ci-runner"
export RC_LIVE_RUNNER_SHA256
RC_LIVE_RUNNER_SHA256=$(sha256sum "$state/reusable-ci-runner" | cut -d' ' -f1)
export RC_LIVE_CONTRACT_FACTS="$frozen_contract_facts"
export RC_LIVE_CA_FACTS="$frozen_ca_facts"
export RC_LIVE_CREDENTIAL_PROXY_BIN="$state/credential-proxy"
export RC_LIVE_PROBE_JSON_BIN="$state/probe-json"

filter=()
[[ -z "$scenario" ]] || filter=(-run "$scenario")
(
	cd -- "$repo_root"
	"$go_bin" test -tags=live -p 1 -count=1 -buildvcs=false -timeout=30m -v "${filter[@]}" ./internal/livetest/...
)
