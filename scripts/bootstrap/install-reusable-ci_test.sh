#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Self-contained tests for install-reusable-ci.sh's cosign-verification
# helper. Exercises the fail-closed contract documented on
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
if [[ "${RC_INSTALLER_TEST_ISOLATED:-}" != "1" ]]; then
	fixture_root="$(/usr/bin/mktemp -d)"
	trap '/usr/bin/rm -rf "$fixture_root"' EXIT
	/usr/bin/mkdir -p "$fixture_root/home"
	status=0
	/usr/bin/env -i PATH=/usr/bin:/bin HOME="$fixture_root/home" TMPDIR="$fixture_root" LC_ALL=C \
		RC_INSTALLER_TEST_ISOLATED=1 /bin/bash --noprofile --norc "${BASH_SOURCE[0]}" || status=$?
	exit "$status"
fi
# Each case uses a subshell to isolate variables and shell options. The shared
# fixture root below handles filesystem cleanup separately.

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

# stub_cosign writes a fake cosign binary into $1/bin that records each
# verify-blob argument list in $1/cosign-calls and exits with the supplied code
# for it (any other subcommand exits 0).
stub_cosign() {
	local dir="$1" verify_exit="$2"
	mkdir -p "$dir/bin"
	{
		printf '#!/bin/sh\nverify_exit=%s\ncalls=%s\n' "$verify_exit" "$dir/cosign-calls"
		cat <<'EOF'
if [ "$1" = "verify-blob" ]; then printf '%s\n' "$*" >>"$calls"; exit "$verify_exit"; fi
exit 0
EOF
	} >"$dir/bin/cosign"
	chmod +x "$dir/bin/cosign"
}

# expect_refusal checks a refusal's status and exact reason on the combined
# output.
expect_refusal() {
	local rc="$1" output="$2" reason="$3"
	if [[ "$rc" == 0 || "$output" != *"$reason"* ]]; then
		printf 'refusal status=%s output=%s, want the reason %s\n' "$rc" "$output" "$reason" >&2
		return 1
	fi
}

# All test-owned state is below one root and removed on every exit. Subshells
# isolate each case's environment; they do not clean up files on their own.
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf "$TEST_ROOT"' EXIT
export TMPDIR="$TEST_ROOT"

t_cosign_absent_fails_by_default() (
	local tmp
	tmp="$(mktemp -d)"
	printf 'fake\n' >"$tmp/checksums.txt"
	printf 'fake bundle\n' >"$tmp/checksums.txt.bundle"
	local output rc=0
	output="$(PATH="$(path_without_cosign)" verify_reusable_ci_cosign "$tmp/checksums.txt" "$tmp/checksums.txt.bundle" 2>&1)" || rc=$?
	if [[ "$rc" == 1 && "$output" == *"cosign not on PATH"* ]]; then
		return 1
	fi
	printf 'unexpected missing-cosign result: %s\n' "$output" >&2
	return 2
)

t_bundle_absent_fails_by_default() (
	local tmp output rc=0
	tmp="$(mktemp -d)"
	printf 'fake\n' >"$tmp/checksums.txt"
	stub_cosign "$tmp" 0
	output="$(PATH="$tmp/bin:$(path_without_cosign)" \
		verify_reusable_ci_cosign "$tmp/checksums.txt" "$tmp/missing.bundle" 2>&1)" || rc=$?
	expect_refusal "$rc" "$output" "missing.bundle missing; the release carries no signature bundle" &&
		[[ ! -e "$tmp/cosign-calls" ]]
)

t_cosign_verify_passes() (
	local tmp
	tmp="$(mktemp -d)"
	printf 'fake\n' >"$tmp/checksums.txt"
	printf 'fake bundle\n' >"$tmp/checksums.txt.bundle"
	stub_cosign "$tmp" 0
	PATH="$tmp/bin:$(path_without_cosign)" \
		verify_reusable_ci_cosign "$tmp/checksums.txt" "$tmp/checksums.txt.bundle" v3.1.0 >/dev/null &&
		[[ "$(cat "$tmp/cosign-calls")" == "verify-blob --bundle $tmp/checksums.txt.bundle --new-bundle-format --certificate-identity-regexp $(_reusable_ci_cosign_identity v3.1.0) --certificate-oidc-issuer https://token.actions.githubusercontent.com $tmp/checksums.txt" ]]
)

t_cosign_verify_rejects() (
	local tmp output rc=0
	tmp="$(mktemp -d)"
	printf 'tampered\n' >"$tmp/checksums.txt"
	printf 'fake bundle\n' >"$tmp/checksums.txt.bundle"
	stub_cosign "$tmp" 1
	output="$(PATH="$tmp/bin:$(path_without_cosign)" \
		verify_reusable_ci_cosign "$tmp/checksums.txt" "$tmp/checksums.txt.bundle" 2>&1)" || rc=$?
	expect_refusal "$rc" "$output" "cosign verification of checksums.txt failed against identity" &&
		[[ "$output" != *"verified"* && "$(wc -l <"$tmp/cosign-calls")" == 1 && "$(cat "$tmp/checksums.txt")" == tampered ]]
)

t_sha256_rejects_tampered() (
	local tmp output rc=0
	tmp="$(mktemp -d)"
	printf 'genuine\n' >"$tmp/app.tgz"
	printf '0000000000000000000000000000000000000000000000000000000000000000  app.tgz\n' >"$tmp/checksums.txt"
	output="$(verify_reusable_ci_sha256 "$tmp/app.tgz" "$tmp/checksums.txt" 2>&1)" || rc=$?
	expect_refusal "$rc" "$output" "app.tgz SHA-256 mismatch:" &&
		[[ "$output" == *"want: 0000000000000000000000000000000000000000000000000000000000000000"* ]]
)

t_sha256_rejects_unlisted_archive() (
	local tmp output rc=0
	tmp="$(mktemp -d)"
	printf 'genuine\n' >"$tmp/app.tgz"
	printf '0000000000000000000000000000000000000000000000000000000000000000  other.tgz\n' >"$tmp/checksums.txt"
	output="$(verify_reusable_ci_sha256 "$tmp/app.tgz" "$tmp/checksums.txt" 2>&1)" || rc=$?
	expect_refusal "$rc" "$output" "app.tgz not in checksums.txt"
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
	local output rc=0
	output="$(REUSABLE_CI_BINARY_SHA256="0000000000000000000000000000000000000000000000000000000000000000" \
		verify_reusable_ci_binary_pin "$tmp/reusable-ci" 2>&1)" || rc=$?
	# fail-closed also removes the mismatching binary
	expect_refusal "$rc" "$output" "installed reusable-ci does not match REUSABLE_CI_BINARY_SHA256:" &&
		[[ ! -e "$tmp/reusable-ci" && "$output" != *"verified"* ]]
)

t_release_ref_does_not_fallback() (
	local tmp
	tmp="$(mktemp -d)"
	# shellcheck disable=SC2329 # invoked indirectly by install_reusable_ci.
	install_reusable_ci_release() { return 1; }
	# shellcheck disable=SC2329 # must remain uncalled; this test detects fallback.
	install_reusable_ci_go_install() {
		: >"$tmp/go-install-called"
		return 0
	}
	if REUSABLE_CI_INSTALL_DIR="$tmp/install" GITHUB_PATH="$tmp/github-path" install_reusable_ci v3.1.0 >/dev/null 2>&1; then
		return 1
	fi
	# Nothing else happened: no go install, no binary, no PATH export.
	[[ ! -e "$tmp/go-install-called" && ! -e "$tmp/install/reusable-ci" && ! -e "$tmp/github-path" ]]
)

t_explicit_release_go_install_is_honored() (
	local tmp
	tmp="$(mktemp -d)"
	# shellcheck disable=SC2329 # invoked indirectly by install_reusable_ci.
	install_reusable_ci_go_install() {
		mkdir -p "$2"
		printf '#!/bin/sh\nprintf "reusable-ci test\\n"\n' >"$2/reusable-ci"
		chmod +x "$2/reusable-ci"
	}
	REUSABLE_CI_USE_GO_INSTALL=1 REUSABLE_CI_INSTALL_DIR="$tmp/install" install_reusable_ci v3.1.0
)

# _reusable_ci_cosign_identity: the two trust domains must each accept their own
# signer's SAN and reject the other's. These grep the SANs against the actual
# returned regex, so they validate the regex, not just a substring.
PRE_SAN_FEAT='https://github.com/diggsweden/reusable-ci/.github/workflows/build-cli.yml@refs/heads/feat/refactor-go'
PRE_SAN_MAIN='https://github.com/diggsweden/reusable-ci/.github/workflows/build-cli.yml@refs/heads/main'
REL_SAN='https://github.com/diggsweden/reusable-ci/.github/workflows/release-binary.yml@refs/heads/main'
# A tag-signed build-cli SAN is not accepted by the current tagged-release
# policy, which trusts release-binary.yml.
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
	# One signer at a time: current tagged releases trust release-binary.yml,
	# not build-cli.yml.
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
assert_exit "bundle absent → fail closed with its reason, cosign never run" 0 t_bundle_absent_fails_by_default
assert_exit "cosign verify-blob exit 0 → accept with the exact verification arguments" 0 t_cosign_verify_passes
assert_exit "cosign verify-blob exit non-0 → reject once with its reason (tampered)" 0 t_cosign_verify_rejects
assert_exit "sha256 verify rejects tampered tarball with its reason" 0 t_sha256_rejects_tampered
assert_exit "sha256 verify rejects an archive missing from checksums" 0 t_sha256_rejects_unlisted_archive
assert_exit "sha256 verify accepts genuine tarball" 0 t_sha256_accepts_genuine
assert_exit "binary pin unset → no-op" 0 t_binary_pin_unset_is_noop
assert_exit "binary pin match → accept" 0 t_binary_pin_accepts_match
assert_exit "binary pin mismatch → fail closed and remove" 0 t_binary_pin_rejects_and_removes
assert_exit "release ref failure does not fall back to go install" 0 t_release_ref_does_not_fallback
assert_exit "explicit release go install is honored" 0 t_explicit_release_go_install_is_honored
assert_exit "pre-release identity accepts build-cli dev-branch SAN" 0 t_identity_pre_accepts_dev_branches
assert_exit "pre-release identity rejects release-binary SAN" 0 t_identity_pre_rejects_release_signer
assert_exit "release identity accepts release-binary main-branch SAN" 0 t_identity_release_accepts_tag_signer
assert_exit "release identity rejects build-cli tag SAN (single signer)" 0 t_identity_release_rejects_build_cli_tag_signer
assert_exit "release identity rejects pre-release build-cli SAN" 0 t_identity_release_rejects_pre_signer
assert_exit "REUSABLE_CI_COSIGN_IDENTITY override wins" 0 t_identity_override_wins

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"

if [[ "$FAIL" -gt 0 ]]; then
	exit 1
fi
