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

readonly expected_schema=2
readonly expected_prefix='rc-'
readonly confirm_action='destroy-live-forge-fixtures'

readonly run_id=${LAB_RUN_ID:?LAB_RUN_ID is required; source a git-provider-lab target contract}
readonly targets=${LAB_TARGETS:?LAB_TARGETS must select at least one provider}
readonly prefix=${LAB_RESOURCE_PREFIX:?LAB_RESOURCE_PREFIX is required}
readonly identity=${LAB_LIVE_EXPECTED_IDENTITY:?LAB_LIVE_EXPECTED_IDENTITY is required}
readonly confirmation=${RC_LIVE_CONFIRM_DESTROY:?RC_LIVE_CONFIRM_DESTROY is required}
readonly cleanup=${LAB_TOKEN_CLEANUP_CMD:?LAB_TOKEN_CLEANUP_CMD is required}

[[ "${LAB_TARGETS_SCHEMA_VERSION:-}" == "$expected_schema" ]] || {
	printf 'LAB_TARGETS_SCHEMA_VERSION must be %s\n' "$expected_schema" >&2
	exit 2
}

[[ "$run_id" =~ ^[a-z0-9][a-z0-9-]{2,39}$ ]] || {
	printf 'LAB_RUN_ID is malformed\n' >&2
	exit 2
}

# The namespace is what binds the destructive guard to resources this suite
# owns. A contract minted for another consumer is well-formed and must still be
# refused here.
[[ "$prefix" == "$expected_prefix" ]] || {
	printf 'this suite owns %s, but the contract declares %s\n' "$expected_prefix" "$prefix" >&2
	exit 2
}

[[ "$identity" == "run=${run_id}|targets="* ]] || {
	printf 'LAB_LIVE_EXPECTED_IDENTITY is not bound to this run\n' >&2
	exit 2
}

[[ "$confirmation" == "${confirm_action}|${identity}" ]] || {
	printf 'RC_LIVE_CONFIRM_DESTROY must equal %s|<LAB_LIVE_EXPECTED_IDENTITY>\n' "$confirm_action" >&2
	exit 2
}

[[ "$cleanup" == /* && -x "$cleanup" && ! -L "$cleanup" ]] || {
	printf 'LAB_TOKEN_CLEANUP_CMD must be an executable absolute path\n' >&2
	exit 2
}

# Every host the run may touch must be a disposable lab host. The suffix is
# fixed here rather than read from the environment: an allowlist supplied by the
# same environment being validated authorises nothing.
selected=0

for kind in gitlab forgejo; do
	case ",${targets}," in
	*",${kind},"*) ;;
	*) continue ;;
	esac

	upper=${kind^^}
	host_var="LAB_${upper}_HOST"
	token_var="LAB_${upper}_TOKEN"
	host=${!host_var:-}

	[[ "${host%%:*}" == *.gitproviderlab ]] || {
		printf '%s host %q is outside the disposable-forge suffix\n' "$kind" "$host" >&2
		exit 2
	}

	[[ -n "${!token_var:-}" ]] || {
		printf '%s token is empty\n' "$kind" >&2
		exit 2
	}

	[[ "$identity" == *"${kind}@https://${host}/"* ]] || {
		printf '%s is selected but absent from the run identity\n' "$kind" >&2
		exit 2
	}

	selected=$((selected + 1))
done

((selected > 0)) || {
	printf 'LAB_TARGETS selects no provider this suite supports (gitlab, forgejo)\n' >&2
	exit 2
}

printf 'live preflight: %s provider(s), run %s, namespace %s\n' "$selected" "$run_id" "$prefix"
