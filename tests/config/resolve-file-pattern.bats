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

@test "get-file-pattern env mode uses explicit pattern when provided" {
  export EXPLICIT_FILE_PATTERN="pom.xml package.json"
  export PROJECT_TYPE="maven"

  run_script "config/get-file-pattern.sh"

  assert_success
  run get_github_output pattern
  assert_output "pom.xml package.json"
}

@test "get-file-pattern env mode falls back to project type" {
  export EXPLICIT_FILE_PATTERN=""
  export PROJECT_TYPE="npm"

  run_script "config/get-file-pattern.sh"

  assert_success
  run get_github_output pattern
  assert_output --partial "package.json"
}
