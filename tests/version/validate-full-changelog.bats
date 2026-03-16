#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/../test_helper.bash"

setup() {
  common_setup_with_github_env
}

teardown() {
  common_teardown
}

@test "validate-full-changelog succeeds when changelog exists" {
  printf 'line one\nline two\n' > "${TEST_DIR}/CHANGELOG.md"

  run_script "version/validate-full-changelog.sh" "${TEST_DIR}/CHANGELOG.md"

  assert_success
  assert_output --partial "Full changelog found"
}

@test "validate-full-changelog fails when changelog is missing" {
  run_script "version/validate-full-changelog.sh" "${TEST_DIR}/CHANGELOG.md"

  assert_failure
  assert_output --partial "Full changelog ("
  assert_output --partial "not found"
}
