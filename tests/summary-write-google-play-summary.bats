#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/test_helper.bash"

setup() {
  common_setup_with_github_env
  export AAB_FILE="${TEST_DIR}/app-release.aab"
  touch "$AAB_FILE"
  export PACKAGE_NAME="se.digg.app"
  export TRACK="production"
  export STATUS="draft"
  export RELEASE_NAME="Release 1"
  export USER_FRACTION="0.25"
  export PRIORITY="3"
}

teardown() {
  common_teardown
}

@test "write-google-play-summary includes rollout details" {
  run_script "summary/write-google-play-summary.sh"

  assert_success
  run get_summary
  assert_output --partial "app-release.aab"
  assert_output --partial "Staged Rollout"
  assert_output --partial "25%"
}

@test "write-google-play-summary includes next-step guidance" {
  run_script "summary/write-google-play-summary.sh"

  assert_success
  run get_summary
  assert_output --partial "Staged rollout to 25% of users will begin after review"
  assert_output --partial "Release is saved as draft"
}
