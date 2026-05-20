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

@test "validate-containerfile writes containerfile output when file exists" {
  touch "${TEST_DIR}/Containerfile"

  run_script "container/validate-containerfile.sh" "${TEST_DIR}/Containerfile"

  assert_success
  run get_github_output containerfile
  assert_output "${TEST_DIR}/Containerfile"
}

@test "validate-containerfile fails when file is missing and no pattern match" {
  run_script "container/validate-containerfile.sh" "${TEST_DIR}/Missingfile"

  assert_failure
  assert_output --partial "Containerfile '${TEST_DIR}/Missingfile' not found"
}

@test "validate-containerfile falls back to Dockerfile* pattern match" {
  touch "${TEST_DIR}/Dockerfile.build"

  run_script "container/validate-containerfile.sh" "${TEST_DIR}/Containerfile"

  assert_success
  run get_github_output containerfile
  assert_output "${TEST_DIR}/Dockerfile.build"
}

@test "validate-containerfile falls back to Containerfile* pattern match" {
  touch "${TEST_DIR}/Containerfile.prod"

  run_script "container/validate-containerfile.sh" "${TEST_DIR}/Containerfile"

  assert_success
  run get_github_output containerfile
  assert_output "${TEST_DIR}/Containerfile.prod"
}

@test "validate-containerfile fails when multiple pattern matches are found" {
  touch "${TEST_DIR}/Dockerfile.build"
  touch "${TEST_DIR}/Containerfile.prod"

  run_script "container/validate-containerfile.sh" "${TEST_DIR}/Containerfile"

  assert_failure
  assert_output --partial "Multiple containerfiles found"
}

@test "validate-containerfile falls back to pattern match in nested directory" {
  mkdir -p "${TEST_DIR}/subdir"
  touch "${TEST_DIR}/subdir/Containerfile.prod"

  run_script "container/validate-containerfile.sh" "${TEST_DIR}/subdir/Containerfile"

  assert_success
  run get_github_output containerfile
  assert_output "${TEST_DIR}/subdir/Containerfile.prod"
}
