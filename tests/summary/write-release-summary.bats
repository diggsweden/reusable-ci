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
  export GITHUB_REPOSITORY="diggsweden/reusable-ci"
  export GITHUB_RUN_ID="12345"
  export RELEASE_VERSION="v1.2.3"
  export RELEASE_BRANCH="main"
  export RELEASE_COMMIT="abcdef123456"
  export RELEASE_ACTOR="octocat"
  export PREPARE_STAGE_RESULT_JSON='{"targets":{"version-bump":"success"}}'
  export BUILD_STAGE_RESULT_JSON='{"targets":{"maven":"success","npm":"skipped","gradle":"failure","gradleandroid":"skipped","xcodeios":"success"}}'
  export PUBLISH_STAGE_RESULT_JSON='{"targets":{"githubpackages":"success","mavencentral":"skipped","appleappstore":"failure","googleplay":"success","containers":"success"}}'
  export CREATE_RELEASE_RESULT="success"
}

teardown() {
  common_teardown
}

@test "write-release-summary writes overview and resources" {
  run_script "summary/write-release-summary.sh"

  assert_success
  run get_summary
  assert_output --partial "Release Summary"
  assert_output --partial "v1.2.3"
  assert_output --partial "Workflow Run"
}

@test "write-release-summary maps results to expected symbols" {
  run_script "summary/write-release-summary.sh"

  assert_success
  run get_summary
  assert_output --partial "| Build Maven | ✓ |"
  assert_output --partial "| Build NPM | − |"
  assert_output --partial "| Build Gradle | ✗ |"
}
