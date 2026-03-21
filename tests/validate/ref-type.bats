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

run_validate_ref_type() {
  run_script "validate/validate-ref-type.sh" "$@"
}

@test "validate-ref-type fails when no arguments provided" {
  run_validate_ref_type

  assert_failure
}

@test "validate-ref-type succeeds for tag ref type" {
  run_validate_ref_type "tag" "v1.0.0" "refs/tags/v1.0.0"

  assert_success
  assert_output --partial "Triggered by tag: v1.0.0"
}

@test "validate-ref-type fails for branch ref type" {
  run_validate_ref_type "branch" "main" "refs/heads/main"

  assert_failure
  assert_output --partial "::error::Release workflow must be triggered by pushing a tag"
}

@test "validate-ref-type shows current trigger on failure" {
  run_validate_ref_type "branch" "develop" "refs/heads/develop"

  assert_failure
  assert_output --partial "Current trigger: branch"
}

@test "validate-ref-type shows tag creation instructions on failure" {
  run_validate_ref_type "branch" "main" "refs/heads/main"

  assert_failure
  assert_output --partial "git tag -s v1.0.0"
  assert_output --partial "git push origin v1.0.0"
}
