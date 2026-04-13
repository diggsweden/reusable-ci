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
  export GITHUB_ACTIONS="true"
  export GITHUB_SHA="abc1234567890"
  export GITHUB_HEAD_REF="feat/my-branch"
  export GITHUB_ACTOR="test-user"
  export GITHUB_REPOSITORY="org/repo"
  export GITHUB_RUN_ID="12345"
  export PROJECT_TYPE="maven"
  export QUALITY_STAGE_RESULT_JSON='{"targets":{"commitlint":"success","licenselint":"success","dependencyreview":"skipped","sastopengrep":"success","megalint":"success","publiccodelint":"success","devbasecheck":"skipped","swift":"skipped"}}'
}

teardown() {
  common_teardown
}

@test "write-pr-summary reads quality result from manifest path when provided" {
  create_test_file "${TEST_DIR}/quality.json" '{"targets":{"commitlint":"failure","licenselint":"success","dependencyreview":"skipped","sastopengrep":"failure","megalint":"success","publiccodelint":"success","devbasecheck":"skipped","swift":"skipped"}}'
  export QUALITY_STAGE_RESULT_PATH="${TEST_DIR}/quality.json"
  export QUALITY_STAGE_RESULT_JSON='{"targets":{"commitlint":"success","licenselint":"success","dependencyreview":"success","sastopengrep":"success","megalint":"failure","publiccodelint":"success","devbasecheck":"success","swift":"success"}}'

  run_script "summary/write-pr-summary.sh"

  assert_success
  run get_summary
  assert_output --partial "| Commit Lint | ✗ |"
  assert_output --partial "| OpenGrep SAST | ✗ |"
}

@test "write-pr-summary creates step summary file" {
  run_script "summary/write-pr-summary.sh"

  assert_success
  assert_file_not_empty "$GITHUB_STEP_SUMMARY"
}

@test "write-pr-summary contains project type" {
  run_script "summary/write-pr-summary.sh"

  assert_success
  assert_summary_contains "maven"
}

@test "write-pr-summary contains short SHA" {
  run_script "summary/write-pr-summary.sh"

  assert_success
  assert_summary_contains "abc1234"
}

@test "write-pr-summary contains branch name" {
  run_script "summary/write-pr-summary.sh"

  assert_success
  assert_summary_contains "feat/my-branch"
}

@test "write-pr-summary contains actor" {
  run_script "summary/write-pr-summary.sh"

  assert_success
  assert_summary_contains "test-user"
}

@test "write-pr-summary contains quality check status table" {
  run_script "summary/write-pr-summary.sh"

  assert_success
  assert_summary_contains "Quality Check Status"
  assert_summary_contains "Commit Lint"
  assert_summary_contains "License Lint"
  assert_summary_contains "OpenGrep SAST"
  assert_summary_contains "MegaLinter"
}

@test "write-pr-summary shows success icon for successful targets" {
  run_script "summary/write-pr-summary.sh"

  assert_success
  run get_summary
  assert_output --partial "| Commit Lint | ✓ |"
  assert_output --partial "| License Lint | ✓ |"
  assert_output --partial "| OpenGrep SAST | ✓ |"
  assert_output --partial "| MegaLinter | ✓ |"
}

@test "write-pr-summary shows failure icon for failed targets" {
  export QUALITY_STAGE_RESULT_JSON='{"targets":{"commitlint":"failure","licenselint":"success","dependencyreview":"skipped","sastopengrep":"failure","megalint":"failure","publiccodelint":"success","devbasecheck":"skipped","swift":"skipped"}}'

  run_script "summary/write-pr-summary.sh"

  assert_success
  run get_summary
  assert_output --partial "| Commit Lint | ✗ |"
  assert_output --partial "| OpenGrep SAST | ✗ |"
  assert_output --partial "| MegaLinter | ✗ |"
  assert_output --partial "| Dependency Review | − |"
}
