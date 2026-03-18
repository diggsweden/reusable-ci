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
  export VERSION="v1.0.0"
  export ARTIFACT_NAME=""
  export REPOSITORY="diggsweden/reusable-ci"
}

teardown() {
  common_teardown
}

run_resolve_release_metadata() {
  run_script "release/resolve-release-metadata.sh"
}

@test "resolve-release-metadata writes expected outputs" {
  export VERSION="v1.2.3"
  export ARTIFACT_NAME="artifact-name"
  export REPOSITORY="diggsweden/reusable-ci"

  run_resolve_release_metadata

  assert_success
  run get_github_output version
  assert_output "v1.2.3"
  run get_github_output version-no-v
  assert_output "1.2.3"
  run get_github_output project-name
  assert_output "artifact-name"
}

@test "resolve-release-metadata falls back to repository basename" {
  export VERSION="v2.0.0"
  export ARTIFACT_NAME=""
  export REPOSITORY="diggsweden/reusable-ci"

  run_resolve_release_metadata

  assert_success
  run get_github_output project-name
  assert_output "reusable-ci"
}

@test "resolve-release-metadata reports fallback choice on stderr" {
  export VERSION="v2.0.0"
  export ARTIFACT_NAME=""
  export REPOSITORY="diggsweden/reusable-ci"

  run_resolve_release_metadata

  assert_success
  assert_stderr_contains "Using repository name: reusable-ci"
}
