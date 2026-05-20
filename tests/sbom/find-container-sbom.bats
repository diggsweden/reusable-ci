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

@test "find-container-sbom writes the found file to GITHUB_OUTPUT" {
  touch "${TEST_DIR}/demo-analyzed-container-sbom.spdx.json"

  run_script "sbom/find-container-sbom.sh"

  assert_success
  run get_github_output sbom-file
  assert_output "demo-analyzed-container-sbom.spdx.json"
}

@test "find-container-sbom fails when no SBOM file exists" {
  run_script "sbom/find-container-sbom.sh"

  assert_failure
  assert_output --partial "No container SBOM file found"
}
