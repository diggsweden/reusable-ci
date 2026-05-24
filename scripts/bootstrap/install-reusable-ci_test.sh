#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Self-contained tests for install-reusable-ci.sh's cosign-verification
# helper. Exercises the soft-skip / fail-closed contract documented on
# verify_reusable_ci_cosign without needing a real GitHub Release or
# the actions/attest-* machinery.
#
# Run:
#   bash scripts/bootstrap/install-reusable-ci_test.sh
#
# Exit 0 on all-pass; non-zero on any failure.

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

t_cosign_absent_soft_skip() (
  local tmp
  tmp="$(mktemp -d)"
  printf 'fake\n' >"$tmp/checksums.txt"
  PATH="$(path_without_cosign)" REUSABLE_CI_REQUIRE_COSIGN=0 \
    verify_reusable_ci_cosign "$tmp/checksums.txt" "$tmp/missing.bundle"
)

t_cosign_absent_fail_closed() (
  local tmp
  tmp="$(mktemp -d)"
  printf 'fake\n' >"$tmp/checksums.txt"
  PATH="$(path_without_cosign)" REUSABLE_CI_REQUIRE_COSIGN=1 \
    verify_reusable_ci_cosign "$tmp/checksums.txt" "$tmp/missing.bundle"
)

t_bundle_absent_soft_skip() (
  local tmp
  tmp="$(mktemp -d)"
  printf 'fake\n' >"$tmp/checksums.txt"
  stub_cosign "$tmp" 0
  PATH="$tmp/bin:$(path_without_cosign)" REUSABLE_CI_REQUIRE_COSIGN=0 \
    verify_reusable_ci_cosign "$tmp/checksums.txt" "$tmp/missing.bundle"
)

t_bundle_absent_fail_closed() (
  local tmp
  tmp="$(mktemp -d)"
  printf 'fake\n' >"$tmp/checksums.txt"
  stub_cosign "$tmp" 0
  PATH="$tmp/bin:$(path_without_cosign)" REUSABLE_CI_REQUIRE_COSIGN=1 \
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

printf 'install-reusable-ci.sh test suite\n'
printf '=================================\n'

assert_exit "cosign absent + REQUIRE_COSIGN=0 → soft-skip" 0 t_cosign_absent_soft_skip
assert_exit "cosign absent + REQUIRE_COSIGN=1 → fail closed" 1 t_cosign_absent_fail_closed
assert_exit "bundle absent + REQUIRE_COSIGN=0 → soft-skip" 0 t_bundle_absent_soft_skip
assert_exit "bundle absent + REQUIRE_COSIGN=1 → fail closed" 1 t_bundle_absent_fail_closed
assert_exit "cosign verify-blob exit 0 → accept" 0 t_cosign_verify_passes
assert_exit "cosign verify-blob exit non-0 → reject (tampered)" 1 t_cosign_verify_rejects
assert_exit "sha256 verify rejects tampered tarball" 1 t_sha256_rejects_tampered
assert_exit "sha256 verify accepts genuine tarball" 0 t_sha256_accepts_genuine

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"

if [[ "$FAIL" -gt 0 ]]; then
  exit 1
fi
