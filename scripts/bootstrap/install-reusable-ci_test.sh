#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

# Self-contained tests for install-reusable-ci.sh's cosign-verification
# helper. Exercises the soft-skip / fail-closed contract documented on
# verify_reusable_ci_cosign without needing a real GitHub Release or
# the actions/attest-* machinery.
#
# Run:
#   bash scripts/bootstrap/install-reusable-ci_test.sh
#
# Exit 0 on all-pass; non-zero on any failure.
#
# Test cases are deliberately subshell-body functions — `t_name() ( ... )` — so
# each runs isolated (a set/cd/var inside one can't leak into another). That is
# the point of the subshell, so silence SC2235's "use { ..; }" overhead note.
# shellcheck disable=SC2235

set -eo pipefail
# Deliberately no `-u`: the test helpers source the installer which
# sets defaults via parameter expansion (${VAR:-default}); set -u
# rejects those even though they're well-defined under bash's
# expansion rules. Each test invocation goes through a subshell so
# inter-test state is fully isolated regardless.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=install-reusable-ci.sh
source "$SCRIPT_DIR/install-reusable-ci.sh"

PASS=0
FAIL=0

# assert_exit runs `cmd` in a subshell and checks its exit code against
# the expected value. Stdout/stderr from cmd are suppressed unless the
# expectation is violated (then they're shown for debugging).
assert_exit() {
	local name="$1" expected="$2"
	shift 2
	local out rc=0
	out="$("$@" 2>&1)" || rc=$?
	if [[ "$rc" == "$expected" ]]; then
		printf '  PASS: %s\n' "$name"
		PASS=$((PASS + 1))
	else
		printf '  FAIL: %s (expected exit=%s, got %s)\n' "$name" "$expected" "$rc" >&2
		printf '    output: %s\n' "$out" >&2
		FAIL=$((FAIL + 1))
	fi
}

# path_without_cosign returns PATH with no directory that contains an
# executable named `cosign`. Used to simulate the "cosign absent" case
# without modifying the developer's environment.
path_without_cosign() {
	local clean=""
	# IFS is scoped to this function via `local`; it doesn't leak to the
	# rest of the script. This is the form the rule's remediation
	# recommends ("set IFS locally using e.g. IFS=',' read -a").
	local IFS=':' # nosemgrep: bash.lang.security.ifs-tampering.ifs-tampering
	local -a parts
	read -ra parts <<<"$PATH"
	for p in "${parts[@]}"; do
		if [[ -d "$p" && ! -x "$p/cosign" ]]; then
			clean="${clean}${clean:+:}${p}"
		fi
	done
	printf '%s' "$clean"
}

# stub_cosign writes a fake cosign binary into $1/bin that exits with
# the supplied code for verify-blob (any other subcommand exits 0).
stub_cosign() {
	local dir="$1" verify_exit="$2"
	mkdir -p "$dir/bin"
	cat >"$dir/bin/cosign" <<EOF
#!/bin/sh
if [ "\$1" = "verify-blob" ]; then exit ${verify_exit}; fi
exit 0
EOF
	chmod +x "$dir/bin/cosign"
}

# Each test is a function that prepares a tmpdir, invokes verify_*,
# and exits with its return code. The subshell isolation in
# assert_exit means we don't need explicit cleanup — tmpdirs leak to
# /tmp which the OS reaps.

t_cosign_absent_fails_by_default() (
	local tmp
	tmp="$(mktemp -d)"
	printf 'fake\n' >"$tmp/checksums.txt"
	PATH="$(path_without_cosign)" \
		verify_reusable_ci_cosign "$tmp/checksums.txt" "$tmp/missing.bundle"
)

t_cosign_absent_allow_unsigned_optout() (
	local tmp
	tmp="$(mktemp -d)"
	printf 'fake\n' >"$tmp/checksums.txt"
	PATH="$(path_without_cosign)" REUSABLE_CI_ALLOW_UNSIGNED=1 \
		verify_reusable_ci_cosign "$tmp/checksums.txt" "$tmp/missing.bundle"
)

t_bundle_absent_fails_by_default() (
	local tmp
	tmp="$(mktemp -d)"
	printf 'fake\n' >"$tmp/checksums.txt"
	stub_cosign "$tmp" 0
	PATH="$tmp/bin:$(path_without_cosign)" \
		verify_reusable_ci_cosign "$tmp/checksums.txt" "$tmp/missing.bundle"
)

t_bundle_absent_allow_unsigned_optout() (
	local tmp
	tmp="$(mktemp -d)"
	printf 'fake\n' >"$tmp/checksums.txt"
	stub_cosign "$tmp" 0
	PATH="$tmp/bin:$(path_without_cosign)" REUSABLE_CI_ALLOW_UNSIGNED=1 \
		verify_reusable_ci_cosign "$tmp/checksums.txt" "$tmp/missing.bundle"
)

t_cosign_verify_passes() (
	local tmp
	tmp="$(mktemp -d)"
	printf 'fake\n' >"$tmp/checksums.txt"
	printf 'fake bundle\n' >"$tmp/checksums.txt.bundle"
	stub_cosign "$tmp" 0
	PATH="$tmp/bin:$(path_without_cosign)" \
		verify_reusable_ci_cosign "$tmp/checksums.txt" "$tmp/checksums.txt.bundle"
)

t_cosign_verify_rejects() (
	local tmp
	tmp="$(mktemp -d)"
	printf 'tampered\n' >"$tmp/checksums.txt"
	printf 'fake bundle\n' >"$tmp/checksums.txt.bundle"
	stub_cosign "$tmp" 1
	PATH="$tmp/bin:$(path_without_cosign)" \
		verify_reusable_ci_cosign "$tmp/checksums.txt" "$tmp/checksums.txt.bundle"
)

t_sha256_rejects_tampered() (
	local tmp
	tmp="$(mktemp -d)"
	printf 'genuine\n' >"$tmp/app.tgz"
	printf '0000000000000000000000000000000000000000000000000000000000000000  app.tgz\n' >"$tmp/checksums.txt"
	verify_reusable_ci_sha256 "$tmp/app.tgz" "$tmp/checksums.txt"
)

t_sha256_accepts_genuine() (
	local tmp hash
	tmp="$(mktemp -d)"
	printf 'genuine\n' >"$tmp/app.tgz"
	if command -v sha256sum &>/dev/null; then
		hash="$(sha256sum "$tmp/app.tgz" | awk '{print $1}')"
	else
		hash="$(shasum -a 256 "$tmp/app.tgz" | awk '{print $1}')"
	fi
	printf '%s  app.tgz\n' "$hash" >"$tmp/checksums.txt"
	verify_reusable_ci_sha256 "$tmp/app.tgz" "$tmp/checksums.txt"
)

t_binary_pin_unset_is_noop() (
	local tmp
	tmp="$(mktemp -d)"
	printf 'binary\n' >"$tmp/reusable-ci"
	REUSABLE_CI_BINARY_SHA256="" verify_reusable_ci_binary_pin "$tmp/reusable-ci"
)

t_binary_pin_accepts_match() (
	local tmp hash
	tmp="$(mktemp -d)"
	printf 'binary\n' >"$tmp/reusable-ci"
	if command -v sha256sum &>/dev/null; then
		hash="$(sha256sum "$tmp/reusable-ci" | awk '{print $1}')"
	else
		hash="$(shasum -a 256 "$tmp/reusable-ci" | awk '{print $1}')"
	fi
	REUSABLE_CI_BINARY_SHA256="$hash" verify_reusable_ci_binary_pin "$tmp/reusable-ci"
)

t_binary_pin_rejects_and_removes() (
	local tmp
	tmp="$(mktemp -d)"
	printf 'binary\n' >"$tmp/reusable-ci"
	if REUSABLE_CI_BINARY_SHA256="0000000000000000000000000000000000000000000000000000000000000000" \
		verify_reusable_ci_binary_pin "$tmp/reusable-ci"; then
		return 1
	fi
	# fail-closed also removes the mismatching binary
	[[ ! -f "$tmp/reusable-ci" ]]
)

# _reusable_ci_cosign_identity: the two trust domains must each accept their own
# signer's SAN and reject the other's. These grep the SANs against the actual
# returned regex, so they validate the regex, not just a substring.
PRE_SAN_FEAT='https://github.com/diggsweden/reusable-ci/.github/workflows/build-cli.yml@refs/heads/feat/refactor-go'
PRE_SAN_MAIN='https://github.com/diggsweden/reusable-ci/.github/workflows/build-cli.yml@refs/heads/main'
REL_SAN='https://github.com/diggsweden/reusable-ci/.github/workflows/release-binary.yml@refs/tags/v3.1.0'
# A tag-signed build-cli SAN: NOT accepted until the build-once cutover
# flips the single release identity to build-cli.yml.
REL_SAN_BUILD_CLI='https://github.com/diggsweden/reusable-ci/.github/workflows/build-cli.yml@refs/tags/v3.1.0'

t_identity_pre_accepts_dev_branches() (
	id="$(_reusable_ci_cosign_identity v3.0.0-pre)"
	printf '%s\n' "$PRE_SAN_FEAT" | grep -Eq "$id" && printf '%s\n' "$PRE_SAN_MAIN" | grep -Eq "$id"
)

t_identity_pre_rejects_release_signer() (
	# A pre-release ref must NOT trust a production-tag signature.
	id="$(_reusable_ci_cosign_identity v3.0.0-pre)"
	! printf '%s\n' "$REL_SAN" | grep -Eq "$id"
)

t_identity_release_accepts_tag_signer() (
	id="$(_reusable_ci_cosign_identity v3.1.0)"
	printf '%s\n' "$REL_SAN" | grep -Eq "$id"
)

t_identity_release_rejects_build_cli_tag_signer() (
	# One signer at a time: a build-cli tag signature is rejected until
	# the build-once cutover flips the release identity to it.
	id="$(_reusable_ci_cosign_identity v3.1.0)"
	! printf '%s\n' "$REL_SAN_BUILD_CLI" | grep -Eq "$id"
)

t_identity_release_rejects_pre_signer() (
	# A production ref must NOT trust a pre-release branch signature.
	id="$(_reusable_ci_cosign_identity v3.1.0)"
	! printf '%s\n' "$PRE_SAN_MAIN" | grep -Eq "$id"
)

t_identity_override_wins() (
	[[ "$(REUSABLE_CI_COSIGN_IDENTITY='OVERRIDE-IDENTITY' _reusable_ci_cosign_identity v3.0.0-pre)" == 'OVERRIDE-IDENTITY' ]]
)

printf 'install-reusable-ci.sh test suite\n'
printf '=================================\n'

assert_exit "cosign absent → fail closed by default" 1 t_cosign_absent_fails_by_default
assert_exit "cosign absent + ALLOW_UNSIGNED=1 → conscious opt-out" 0 t_cosign_absent_allow_unsigned_optout
assert_exit "bundle absent → fail closed by default" 1 t_bundle_absent_fails_by_default
assert_exit "bundle absent + ALLOW_UNSIGNED=1 → conscious opt-out" 0 t_bundle_absent_allow_unsigned_optout
assert_exit "cosign verify-blob exit 0 → accept" 0 t_cosign_verify_passes
assert_exit "cosign verify-blob exit non-0 → reject (tampered)" 1 t_cosign_verify_rejects
assert_exit "sha256 verify rejects tampered tarball" 1 t_sha256_rejects_tampered
assert_exit "sha256 verify accepts genuine tarball" 0 t_sha256_accepts_genuine
assert_exit "binary pin unset → no-op" 0 t_binary_pin_unset_is_noop
assert_exit "binary pin match → accept" 0 t_binary_pin_accepts_match
assert_exit "binary pin mismatch → fail closed and remove" 0 t_binary_pin_rejects_and_removes
assert_exit "pre-release identity accepts build-cli dev-branch SAN" 0 t_identity_pre_accepts_dev_branches
assert_exit "pre-release identity rejects release-binary SAN" 0 t_identity_pre_rejects_release_signer
assert_exit "release identity accepts release-binary tag SAN" 0 t_identity_release_accepts_tag_signer
assert_exit "release identity rejects build-cli tag SAN (single signer)" 0 t_identity_release_rejects_build_cli_tag_signer
assert_exit "release identity rejects pre-release build-cli SAN" 0 t_identity_release_rejects_pre_signer
assert_exit "REUSABLE_CI_COSIGN_IDENTITY override wins" 0 t_identity_override_wins

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"

if [[ "$FAIL" -gt 0 ]]; then
	exit 1
fi
