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
  export PACKAGE_NAME="demo-package"
  export VERSION="1.2.3"
  export NODE_VERSION="22"
  export SKIP_TESTS="false"
}

teardown() {
  common_teardown
}

@test "write-npm-build-summary writes key build details" {
  run_script "summary/write-npm-build-summary.sh"

  assert_success
  run get_summary
  assert_output --partial "NPM Build Summary"
  assert_output --partial 'demo-package@1.2.3'
  assert_output --partial "Node.js:** 22"
}

@test "write-npm-build-summary reflects skipped tests" {
  export SKIP_TESTS="true"

  run_script "summary/write-npm-build-summary.sh"

  assert_success
  run get_summary
  assert_output --partial "⊘ Skipped"
}
