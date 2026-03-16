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

@test "all builds succeed with artifacts present" {
  export BUILD_MAVEN_RESULT="success"
  export BUILD_NPM_RESULT="success"
  export BUILD_GRADLE_RESULT="success"
  export BUILD_GRADLE_ANDROID_RESULT="success"
  export BUILD_XCODE_RESULT="success"
  export MAVEN_ARTIFACTS='[{"name":"app.jar"}]'
  export NPM_ARTIFACTS='[{"name":"app.tgz"}]'
  export GRADLE_ARTIFACTS='[{"name":"app.jar"}]'
  export GRADLEANDROID_ARTIFACTS='[{"name":"app.apk"}]'
  export XCODEIOS_ARTIFACTS='[{"name":"app.ipa"}]'

  run_script "summary/write-build-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "true"

  run get_github_output "stage-result"
  assert_output "success"
}

@test "all skipped by default when no env vars set" {
  run_script "summary/write-build-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "false"

  run get_github_output "stage-result"
  assert_output "skipped"
}

@test "single failure propagates to stage result" {
  export BUILD_MAVEN_RESULT="success"
  export BUILD_NPM_RESULT="failure"
  export BUILD_GRADLE_RESULT="success"
  export MAVEN_ARTIFACTS='[{"name":"app.jar"}]'
  export NPM_ARTIFACTS='[{"name":"app.tgz"}]'

  run_script "summary/write-build-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "true"

  run get_github_output "stage-result"
  assert_output "failure"
}

@test "single cancelled propagates to stage result" {
  export BUILD_MAVEN_RESULT="success"
  export BUILD_GRADLE_RESULT="cancelled"
  export MAVEN_ARTIFACTS='[{"name":"app.jar"}]'
  export GRADLE_ARTIFACTS='[{"name":"app.jar"}]'

  run_script "summary/write-build-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "true"

  run get_github_output "stage-result"
  assert_output "cancelled"
}

@test "failure takes precedence over cancelled" {
  export BUILD_MAVEN_RESULT="failure"
  export BUILD_NPM_RESULT="cancelled"
  export MAVEN_ARTIFACTS='[{"name":"app.jar"}]'
  export NPM_ARTIFACTS='[{"name":"app.tgz"}]'

  run_script "summary/write-build-stage-result.sh"
  assert_success

  run get_github_output "stage-result"
  assert_output "failure"
}

@test "unknown result values normalize to skipped" {
  export BUILD_MAVEN_RESULT="weird_value"
  export BUILD_NPM_RESULT="in_progress"
  export MAVEN_ARTIFACTS='[{"name":"app.jar"}]'

  run_script "summary/write-build-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "true"

  run get_github_output "stage-result"
  assert_output "success"
}

@test "stage-ran is true when only one artifact type is non-empty" {
  export BUILD_XCODE_RESULT="success"
  export XCODEIOS_ARTIFACTS='[{"name":"app.ipa"}]'

  run_script "summary/write-build-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "true"

  run get_github_output "stage-result"
  assert_output "success"
}

@test "stage-ran is false when all artifacts are empty arrays" {
  export BUILD_MAVEN_RESULT="success"
  export MAVEN_ARTIFACTS='[]'
  export NPM_ARTIFACTS='[]'
  export GRADLE_ARTIFACTS='[]'
  export GRADLEANDROID_ARTIFACTS='[]'
  export XCODEIOS_ARTIFACTS='[]'

  run_script "summary/write-build-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "false"

  run get_github_output "stage-result"
  assert_output "skipped"
}

@test "result-json contains expected structure with all targets" {
  export BUILD_MAVEN_RESULT="success"
  export BUILD_NPM_RESULT="failure"
  export BUILD_GRADLE_RESULT="skipped"
  export BUILD_GRADLE_ANDROID_RESULT="cancelled"
  export BUILD_XCODE_RESULT="success"
  export MAVEN_ARTIFACTS='[{"name":"app.jar"}]'
  export NPM_ARTIFACTS='[{"name":"app.tgz"}]'

  run_script "summary/write-build-stage-result.sh"
  assert_success

  run get_github_output "result-json"
  assert_output --partial '"stage":"build"'
  assert_output --partial '"result":"failure"'
  assert_output --partial '"ran":true'
  assert_output --partial '"maven":"success"'
  assert_output --partial '"npm":"failure"'
  assert_output --partial '"gradle":"skipped"'
  assert_output --partial '"gradleandroid":"cancelled"'
  assert_output --partial '"xcodeios":"success"'
}

@test "result-json ran is false when stage did not run" {
  run_script "summary/write-build-stage-result.sh"
  assert_success

  run get_github_output "result-json"
  assert_output --partial '"ran":false'
  assert_output --partial '"result":"skipped"'
}

@test "result values without artifacts still report skipped stage" {
  export BUILD_MAVEN_RESULT="success"
  export BUILD_NPM_RESULT="success"

  run_script "summary/write-build-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "false"

  run get_github_output "stage-result"
  assert_output "skipped"
}
