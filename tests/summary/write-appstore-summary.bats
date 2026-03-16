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
  export IPA_FILE="${TEST_DIR}/app.ipa"
  touch "$IPA_FILE"
  export PLATFORM="ios"
  export SKIP_VALIDATION="true"
  export SUBMIT_REVIEW="false"
  export REQUEST_ID="request-123"
}

teardown() {
  common_teardown
}

@test "write-appstore-summary includes validation and request details" {
  run_script "summary/write-appstore-summary.sh"

  assert_success
  run get_summary
  assert_output --partial "app.ipa"
  assert_output --partial "⊘ Skipped"
  assert_output --partial "request-123"
}

@test "write-appstore-summary includes manual submission guidance" {
  run_script "summary/write-appstore-summary.sh"

  assert_success
  run get_summary
  assert_output --partial "Manually submit for external testing or App Store review"
}
