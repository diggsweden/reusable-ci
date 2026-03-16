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

@test "all linters succeed when enabled" {
  export COMMITLINT_RESULT="success"
  export LICENSLINT_RESULT="success"
  export DEPENDENCYREVIEW_RESULT="success"
  export MEGALINT_RESULT="success"
  export PUBLICCODELINT_RESULT="success"
  export DEVBASECHECK_RESULT="success"
  export SWIFT_RESULT="success"
  export COMMITLINT_ENABLED="true"
  export LICENSLINT_ENABLED="true"
  export DEPENDENCYREVIEW_ENABLED="true"
  export MEGALINT_ENABLED="true"
  export PUBLICCODELINT_ENABLED="true"
  export DEVBASECHECK_ENABLED="true"
  export SWIFT_ENABLED="true"

  run_script "summary/write-pr-quality-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "true"

  run get_github_output "stage-result"
  assert_output "success"
}

@test "all default to success when no env vars set" {
  run_script "summary/write-pr-quality-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "true"

  run get_github_output "stage-result"
  assert_output "success"
}

@test "single failure propagates to stage result" {
  export COMMITLINT_RESULT="success"
  export LICENSLINT_RESULT="success"
  export DEPENDENCYREVIEW_RESULT="failure"
  export MEGALINT_RESULT="success"
  export PUBLICCODELINT_RESULT="success"
  export DEVBASECHECK_RESULT="success"
  export SWIFT_RESULT="success"

  run_script "summary/write-pr-quality-stage-result.sh"
  assert_success

  run get_github_output "stage-result"
  assert_output "failure"
}

@test "single cancelled propagates to stage result" {
  export COMMITLINT_RESULT="success"
  export LICENSLINT_RESULT="success"
  export DEPENDENCYREVIEW_RESULT="success"
  export MEGALINT_RESULT="cancelled"
  export PUBLICCODELINT_RESULT="success"
  export DEVBASECHECK_RESULT="success"
  export SWIFT_RESULT="success"

  run_script "summary/write-pr-quality-stage-result.sh"
  assert_success

  run get_github_output "stage-result"
  assert_output "cancelled"
}

@test "failure takes precedence over cancelled" {
  export COMMITLINT_RESULT="failure"
  export MEGALINT_RESULT="cancelled"

  run_script "summary/write-pr-quality-stage-result.sh"
  assert_success

  run get_github_output "stage-result"
  assert_output "failure"
}

@test "unknown result values normalize to skipped" {
  export COMMITLINT_RESULT="weird_value"
  export LICENSLINT_RESULT="in_progress"

  run_script "summary/write-pr-quality-stage-result.sh"
  assert_success

  run get_github_output "stage-result"
  assert_output "success"
}

@test "stage-ran is always true" {
  run_script "summary/write-pr-quality-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "true"
}

@test "disabled linters appear as skipped in result-json" {
  export COMMITLINT_RESULT="success"
  export LICENSLINT_RESULT="success"
  export COMMITLINT_ENABLED="true"
  export LICENSLINT_ENABLED="false"
  export DEPENDENCYREVIEW_ENABLED="false"
  export MEGALINT_ENABLED="false"
  export PUBLICCODELINT_ENABLED="false"
  export DEVBASECHECK_ENABLED="false"
  export SWIFT_ENABLED="false"

  run_script "summary/write-pr-quality-stage-result.sh"
  assert_success

  run get_github_output "result-json"
  assert_output --partial '"commitlint":"success"'
  assert_output --partial '"licenselint":"skipped"'
  assert_output --partial '"dependencyreview":"skipped"'
  assert_output --partial '"megalint":"skipped"'
}

@test "enabled linters show their actual result in result-json" {
  export COMMITLINT_RESULT="success"
  export LICENSLINT_RESULT="failure"
  export DEPENDENCYREVIEW_RESULT="cancelled"
  export MEGALINT_RESULT="success"
  export COMMITLINT_ENABLED="true"
  export LICENSLINT_ENABLED="true"
  export DEPENDENCYREVIEW_ENABLED="true"
  export MEGALINT_ENABLED="true"
  export PUBLICCODELINT_ENABLED="false"
  export DEVBASECHECK_ENABLED="false"
  export SWIFT_ENABLED="false"

  run_script "summary/write-pr-quality-stage-result.sh"
  assert_success

  run get_github_output "result-json"
  assert_output --partial '"commitlint":"success"'
  assert_output --partial '"licenselint":"failure"'
  assert_output --partial '"dependencyreview":"cancelled"'
  assert_output --partial '"megalint":"success"'
}

@test "result-json contains expected structure" {
  export COMMITLINT_RESULT="success"
  export COMMITLINT_ENABLED="true"
  export LICENSLINT_ENABLED="false"
  export DEPENDENCYREVIEW_ENABLED="false"
  export MEGALINT_ENABLED="false"
  export PUBLICCODELINT_ENABLED="false"
  export DEVBASECHECK_ENABLED="false"
  export SWIFT_ENABLED="false"

  run_script "summary/write-pr-quality-stage-result.sh"
  assert_success

  run get_github_output "result-json"
  assert_output --partial '"stage":"pr-quality"'
  assert_output --partial '"result":"success"'
  assert_output --partial '"ran":true'
  assert_output --partial '"commitlint":"success"'
  assert_output --partial '"licenselint":"skipped"'
  assert_output --partial '"dependencyreview":"skipped"'
  assert_output --partial '"megalint":"skipped"'
  assert_output --partial '"publiccodelint":"skipped"'
  assert_output --partial '"devbasecheck":"skipped"'
  assert_output --partial '"swift":"skipped"'
}

@test "stage-result reflects raw results regardless of enabled flags" {
  export COMMITLINT_RESULT="failure"
  export COMMITLINT_ENABLED="false"

  run_script "summary/write-pr-quality-stage-result.sh"
  assert_success

  run get_github_output "stage-result"
  assert_output "failure"

  run get_github_output "result-json"
  assert_output --partial '"commitlint":"skipped"'
}
