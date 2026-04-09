#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/../test_helper.bash"

setup() {
  common_setup
}

teardown() {
  common_teardown
}

@test "manifest helper writes and reads stage result in default directory" {
  run --separate-stderr bash -c 'source "$1"; path="$(ci_write_stage_result "build" "{\"result\":\"success\"}")"; printf "path=%s\ncontent=%s\n" "$path" "$(ci_read_stage_result "build")"' _ "$SCRIPTS_DIR/ci/manifest.sh"

  assert_success
  assert_output --partial "path=.ci-results/build-result.json"
  assert_output --partial 'content={"result":"success"}'
  assert_file_exist "$TEST_DIR/.ci-results/build-result.json"
}

@test "manifest helper respects CI_RESULTS_DIR override" {
  export CI_RESULTS_DIR="custom-results"

  run --separate-stderr bash -c 'source "$1"; printf "%s\n" "$(ci_write_manifest "custom" "{\"ok\":true}")"' _ "$SCRIPTS_DIR/ci/manifest.sh"

  assert_success
  assert_output "custom-results/custom.json"
  assert_file_exist "$TEST_DIR/custom-results/custom.json"
}
