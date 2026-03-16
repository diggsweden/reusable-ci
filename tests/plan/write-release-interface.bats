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

set_all_flags_true() {
  export FIRST_PROJECT_TYPE="maven"
  export FIRST_BUILD_TYPE="jar"
  export FIRST_ARTIFACT_NAME="myapp"
  export SHOULD_SIGN_ARTIFACTS="true"
  export SHOULD_CHECK_AUTHORIZATION="true"
  export SHOULD_RUN_VERSION_BUMP="true"
  export SHOULD_CREATE_GITHUB_RELEASE="true"
  export SHOULD_CREATE_DRAFT_RELEASE="true"
  export SHOULD_GENERATE_SBOM="true"
  export SHOULD_MAKE_LATEST="true"
  export HAS_CONTAINERS="true"
}

set_all_flags_false() {
  export FIRST_PROJECT_TYPE="maven"
  export FIRST_BUILD_TYPE="jar"
  export FIRST_ARTIFACT_NAME="myapp"
  export SHOULD_SIGN_ARTIFACTS="false"
  export SHOULD_CHECK_AUTHORIZATION="false"
  export SHOULD_RUN_VERSION_BUMP="false"
  export SHOULD_CREATE_GITHUB_RELEASE="false"
  export SHOULD_CREATE_DRAFT_RELEASE="false"
  export SHOULD_GENERATE_SBOM="false"
  export SHOULD_MAKE_LATEST="false"
  export HAS_CONTAINERS="false"
}

@test "write-release-interface succeeds with all flags true" {
  set_all_flags_true

  run_script "plan/write-release-interface.sh"

  assert_success
}

@test "release-context-json contains project type, build type, and artifact name" {
  set_all_flags_true
  export FIRST_PROJECT_TYPE="npm"
  export FIRST_BUILD_TYPE="tarball"
  export FIRST_ARTIFACT_NAME="my-library"

  run_script "plan/write-release-interface.sh"
  assert_success

  run get_github_output "release-context-json"
  assert_output --partial '"project_type":"npm"'
  assert_output --partial '"build_type":"tarball"'
  assert_output --partial '"artifact_name":"my-library"'
}

@test "release-policy-json has correct booleans when all flags true" {
  set_all_flags_true

  run_script "plan/write-release-interface.sh"
  assert_success

  run get_github_output "release-policy-json"
  assert_output --partial '"sign_artifacts":true'
  assert_output --partial '"check_authorization":true'
  assert_output --partial '"run_version_bump":true'
  assert_output --partial '"create_github_release":true'
  assert_output --partial '"create_draft_release":true'
  assert_output --partial '"generate_sbom":true'
  assert_output --partial '"make_latest":true'
  assert_output --partial '"has_containers":true'
}

@test "all flags false produces all false in policy" {
  set_all_flags_false

  run_script "plan/write-release-interface.sh"
  assert_success

  run get_github_output "release-policy-json"
  assert_output '{"sign_artifacts":false,"check_authorization":false,"run_version_bump":false,"create_github_release":false,"create_draft_release":false,"generate_sbom":false,"make_latest":false,"has_containers":false}'
}

@test "mixed true/false flags are correctly converted" {
  set_all_flags_false
  export SHOULD_SIGN_ARTIFACTS="true"
  export SHOULD_GENERATE_SBOM="true"
  export HAS_CONTAINERS="true"

  run_script "plan/write-release-interface.sh"
  assert_success

  run get_github_output "release-policy-json"
  assert_output --partial '"sign_artifacts":true'
  assert_output --partial '"check_authorization":false'
  assert_output --partial '"run_version_bump":false'
  assert_output --partial '"create_github_release":false'
  assert_output --partial '"create_draft_release":false'
  assert_output --partial '"generate_sbom":true'
  assert_output --partial '"make_latest":false'
  assert_output --partial '"has_containers":true'
}
