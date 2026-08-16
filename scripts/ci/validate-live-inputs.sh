#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

# Pure preflight for the live-forge conformance tier. It must complete before
# the suite builds a product or reaches any provider mutation, and it reads only
# — nothing here creates, deletes, or authenticates.
#
# The Go guard in internal/livetest re-derives all of this independently. That
# duplication is deliberate: this runs before a binary exists, so it is the
# check that stops a misconfigured run at the door, while the Go guard is what
# stands between a scenario and a real DELETE.
set -euo pipefail

readonly expected_version=1
readonly expected_prefix='rc-'
readonly confirm_action='destroy-live-forge-fixtures'

# The neutral live-target contract: infrastructure facts only. Everything that
# authorises destruction -- the namespace, the owners, the confirmation -- is
# this suite's own, because a producer that also supplied them would be handing
# out permission along with the address.
readonly contract=${LAB_TARGETS_FILE:?LAB_TARGETS_FILE must name a neutral live-target contract}
readonly confirmation=${RC_LIVE_CONFIRM_DESTROY:?RC_LIVE_CONFIRM_DESTROY is required}

[[ "$contract" == /* && -f "$contract" && ! -L "$contract" ]] || {
	printf 'LAB_TARGETS_FILE must be an absolute path to a regular file\n' >&2
	exit 2
}

command -v jq >/dev/null 2>&1 || {
	printf 'live preflight needs jq to read the contract; it is pinned in .mise.toml\n' >&2
	exit 2
}

version=$(jq -r '.version // empty' <"$contract")
[[ "$version" == "$expected_version" ]] || {
	printf 'live-target contract version must be %s, got %s\n' "$expected_version" "${version:-none}" >&2
	exit 2
}

run_id=$(jq -r '.generation.id // empty' <"$contract")
[[ "$run_id" =~ ^[a-z0-9][a-z0-9-]{2,39}$ ]] || {
	printf 'contract generation.id is malformed\n' >&2
	exit 2
}
readonly run_id

cleanup=$(jq -r '.interfaces.credential_cleanup // empty' <"$contract")
[[ "$cleanup" == /* && -x "$cleanup" && ! -L "$cleanup" ]] || {
	printf 'contract credential_cleanup must be an executable absolute path\n' >&2
	exit 2
}
readonly cleanup

# Rebuilt here, not read: the identity is what the confirmation is typed
# against, so a preflight that read it would be checking a claim against
# itself. The Go guard derives the same string independently.
identity="run=${run_id}|targets="
readonly prefix="$expected_prefix"

# Every host the run may touch must be a disposable lab host, and every forge
# it may act on must have an owner the operator declared. The pattern is fixed
# here rather than read from the environment: an allowlist supplied by the same
# environment being validated authorises nothing.
selected=0
entries=()

while IFS=$'\t' read -r kind api_base; do
	[[ -n "$kind" ]] || continue

	case "$kind" in
	gitlab | forgejo) ;;
	*) continue ;;
	esac

	owner_var="RC_LIVE_${kind^^}_OWNER"
	owner=${!owner_var:-}
	[[ -n "$owner" ]] || continue

	host=${api_base#https://}
	host=${host%%/*}

	[[ "${host%%:*}" =~ ^(gitlab|gitea|forgejo)\.(compose|k3s)\.forgelab$ ]] || {
		printf '%s host %q is not a disposable lab forge\n' "$kind" "$host" >&2
		exit 2
	}

	[[ -n "$(jq -r --arg k "$kind" '.endpoints[] | select(.kind==$k) | .credential.token // empty' <"$contract")" ]] || {
		printf '%s credential carries no token\n' "$kind" >&2
		exit 2
	}

	entries+=("${kind}@https://${host}/${owner}#resources=${prefix}")
	selected=$((selected + 1))
done < <(jq -r '.endpoints[] | [.kind, .api_base_url] | @tsv' <"$contract")

((selected > 0)) || {
	printf 'no contract endpoint has a declared RC_LIVE_<FORGE>_OWNER (gitlab, forgejo)\n' >&2
	exit 2
}

# Sorted so the identity does not depend on the order the producer wrote the
# endpoints in; the Go guard sorts the same way.
readarray -t sorted < <(printf '%s\n' "${entries[@]}" | LC_ALL=C sort)
identity+=$(
	IFS=,
	printf '%s' "${sorted[*]}"
)
readonly identity

[[ "$confirmation" == "${confirm_action}|${identity}" ]] || {
	printf 'RC_LIVE_CONFIRM_DESTROY must equal %s|%s\n' "$confirm_action" "$identity" >&2
	exit 2
}

# The binaries the product shells out to during this tier.
#
# Checked here rather than left to the scenario that needs one: a missing tool
# surfaces as `exec: "syft": executable file not found` several minutes into a
# destructive run, which reads as a broken product rather than an unprepared
# host -- and by then the run has already seeded fixtures it must tear down.
# Every one of these is pinned in .mise.toml, so the fix is always the same.
missing=()
for tool in cosign syft buildah skopeo; do
	command -v "$tool" >/dev/null 2>&1 || missing+=("$tool")
done

((${#missing[@]} == 0)) || {
	printf 'live preflight: the product shells out to these, and they are not on PATH: %s\n' "${missing[*]}" >&2
	printf 'They are pinned in .mise.toml; install them with: mise install\n' >&2
	exit 2
}

printf 'live preflight: %s provider(s), generation %s, namespace %s\n' "$selected" "$run_id" "$prefix"
