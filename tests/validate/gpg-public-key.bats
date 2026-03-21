#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/../test_helper.bash"

setup() {
  common_setup
  setup_github_env
}

teardown() {
  common_teardown
}

run_validate_gpg_public_key() {
  run_script "validate/validate-gpg-public-key.sh" "$@"
}

@test "validate-gpg-public-key fails when secret is not set" {
  unset OSPO_BOT_GPG_PUB

  run_validate_gpg_public_key

  assert_failure
  assert_output --partial "::error::Missing OSPO_BOT_GPG_PUB secret"
}

@test "validate-gpg-public-key fails when secret is empty" {
  export OSPO_BOT_GPG_PUB=""

  run_validate_gpg_public_key

  assert_failure
  assert_output --partial "Missing OSPO_BOT_GPG_PUB secret"
}

@test "validate-gpg-public-key shows setup instructions on failure" {
  unset OSPO_BOT_GPG_PUB

  run_validate_gpg_public_key

  assert_failure
  assert_output --partial "Settings"
  assert_output --partial "Actions"
}

@test "validate-gpg-public-key succeeds when secret is configured" {
  export OSPO_BOT_GPG_PUB="-----BEGIN PGP PUBLIC KEY BLOCK-----"

  run_validate_gpg_public_key

  assert_success
  assert_output --partial "GPG public key configured"
}
