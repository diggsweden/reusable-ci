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

@test "maven project with success result" {
  export PROJECT_TYPE="maven"
  export BUILD_MAVEN_DEV_RESULT="success"

  run_script "summary/write-dev-build-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "true"

  run get_github_output "stage-result"
  assert_output "success"
}

@test "npm project with success result" {
  export PROJECT_TYPE="npm"
  export BUILD_NPM_DEV_RESULT="success"

  run_script "summary/write-dev-build-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "true"

  run get_github_output "stage-result"
  assert_output "success"
}

@test "gradle project with success result" {
  export PROJECT_TYPE="gradle"
  export BUILD_GRADLE_DEV_RESULT="success"

  run_script "summary/write-dev-build-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "true"

  run get_github_output "stage-result"
  assert_output "success"
}

@test "all skipped by default when no env vars set" {
  run_script "summary/write-dev-build-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "false"

  run get_github_output "stage-result"
  assert_output "skipped"
}

@test "failure propagates for maven project" {
  export PROJECT_TYPE="maven"
  export BUILD_MAVEN_DEV_RESULT="failure"

  run_script "summary/write-dev-build-stage-result.sh"
  assert_success

  run get_github_output "stage-result"
  assert_output "failure"
}

@test "cancelled propagates for npm project" {
  export PROJECT_TYPE="npm"
  export BUILD_NPM_DEV_RESULT="cancelled"

  run_script "summary/write-dev-build-stage-result.sh"
  assert_success

  run get_github_output "stage-result"
  assert_output "cancelled"
}

@test "unknown result values normalize to skipped" {
  export PROJECT_TYPE="maven"
  export BUILD_MAVEN_DEV_RESULT="bogus_status"

  run_script "summary/write-dev-build-stage-result.sh"
  assert_success

  run get_github_output "stage-result"
  assert_output "skipped"
}

@test "unknown project type means stage did not run" {
  export PROJECT_TYPE="python"
  export BUILD_MAVEN_DEV_RESULT="success"

  run_script "summary/write-dev-build-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "false"

  run get_github_output "stage-result"
  assert_output "skipped"
}

@test "stage-ran is true for known project types" {
  for pt in maven npm gradle; do
    export PROJECT_TYPE="$pt"
    : >"$GITHUB_OUTPUT"

    run_script "summary/write-dev-build-stage-result.sh"
    assert_success

    run get_github_output "stage-ran"
    assert_output "true"
  done
}

@test "result-json contains expected structure" {
  export PROJECT_TYPE="npm"
  export BUILD_NPM_DEV_RESULT="success"
  export BUILD_MAVEN_DEV_RESULT="skipped"
  export BUILD_GRADLE_DEV_RESULT="skipped"

  run_script "summary/write-dev-build-stage-result.sh"
  assert_success

  run get_github_output "result-json"
  assert_output --partial '"stage":"dev-build"'
  assert_output --partial '"result":"success"'
  assert_output --partial '"ran":true'
  assert_output --partial '"project_type":"npm"'
  assert_output --partial '"npm":"success"'
  assert_output --partial '"maven":"skipped"'
  assert_output --partial '"gradle":"skipped"'
}

@test "result-json ran is false for unknown project type" {
  export PROJECT_TYPE="rust"

  run_script "summary/write-dev-build-stage-result.sh"
  assert_success

  run get_github_output "result-json"
  assert_output --partial '"ran":false'
  assert_output --partial '"result":"skipped"'
  assert_output --partial '"project_type":"rust"'
}

@test "only the matching project type result is used for stage-result" {
  export PROJECT_TYPE="maven"
  export BUILD_MAVEN_DEV_RESULT="success"
  export BUILD_NPM_DEV_RESULT="failure"
  export BUILD_GRADLE_DEV_RESULT="cancelled"

  run_script "summary/write-dev-build-stage-result.sh"
  assert_success

  run get_github_output "stage-result"
  assert_output "success"
}
