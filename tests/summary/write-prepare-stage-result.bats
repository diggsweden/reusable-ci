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

@test "success when version bump enabled and artifacts present" {
  export PREPARE_RELEASE_RESULT="success"
  export SHOULD_RUN_VERSION_BUMP="true"
  export ARTIFACTS='[{"name":"app.jar"}]'

  run_script "summary/write-prepare-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "true"

  run get_github_output "stage-result"
  assert_output "success"
}

@test "all skipped by default when no env vars set" {
  run_script "summary/write-prepare-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "false"

  run get_github_output "stage-result"
  assert_output "skipped"
}

@test "failure propagates when stage ran" {
  export PREPARE_RELEASE_RESULT="failure"
  export SHOULD_RUN_VERSION_BUMP="true"
  export ARTIFACTS='[{"name":"app.jar"}]'

  run_script "summary/write-prepare-stage-result.sh"
  assert_success

  run get_github_output "stage-result"
  assert_output "failure"
}

@test "cancelled propagates when stage ran" {
  export PREPARE_RELEASE_RESULT="cancelled"
  export SHOULD_RUN_VERSION_BUMP="true"
  export ARTIFACTS='[{"name":"app.jar"}]'

  run_script "summary/write-prepare-stage-result.sh"
  assert_success

  run get_github_output "stage-result"
  assert_output "cancelled"
}

@test "unknown result values normalize to skipped" {
  export PREPARE_RELEASE_RESULT="in_progress"
  export SHOULD_RUN_VERSION_BUMP="true"
  export ARTIFACTS='[{"name":"app.jar"}]'

  run_script "summary/write-prepare-stage-result.sh"
  assert_success

  run get_github_output "stage-result"
  assert_output "skipped"
}

@test "stage-ran is false when version bump is false" {
  export PREPARE_RELEASE_RESULT="success"
  export SHOULD_RUN_VERSION_BUMP="false"
  export ARTIFACTS='[{"name":"app.jar"}]'

  run_script "summary/write-prepare-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "false"

  run get_github_output "stage-result"
  assert_output "skipped"
}

@test "stage-ran is false when artifacts are empty" {
  export PREPARE_RELEASE_RESULT="success"
  export SHOULD_RUN_VERSION_BUMP="true"
  export ARTIFACTS='[]'

  run_script "summary/write-prepare-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "false"

  run get_github_output "stage-result"
  assert_output "skipped"
}

@test "stage-ran is false when both conditions are unmet" {
  export PREPARE_RELEASE_RESULT="success"
  export SHOULD_RUN_VERSION_BUMP="false"
  export ARTIFACTS='[]'

  run_script "summary/write-prepare-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "false"
}

@test "result-json contains expected structure when ran" {
  export PREPARE_RELEASE_RESULT="success"
  export SHOULD_RUN_VERSION_BUMP="true"
  export ARTIFACTS='[{"name":"app.jar"}]'

  run_script "summary/write-prepare-stage-result.sh"
  assert_success

  run get_github_output "result-json"
  assert_output --partial '"stage":"prepare"'
  assert_output --partial '"result":"success"'
  assert_output --partial '"ran":true'
  assert_output --partial '"version-bump":"success"'
}

@test "result-json ran is false when stage did not run" {
  export SHOULD_RUN_VERSION_BUMP="false"

  run_script "summary/write-prepare-stage-result.sh"
  assert_success

  run get_github_output "result-json"
  assert_output --partial '"ran":false'
  assert_output --partial '"result":"skipped"'
}
