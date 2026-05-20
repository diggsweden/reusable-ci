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
  export BUILD_TYPE="lib"
  export GROUP_ID="se.digg"
  export ARTIFACT_ID="demo"
  export VERSION="1.2.3"
  export JAVA_VERSION="21"
  export SKIP_TESTS="false"
  export IS_SNAPSHOT="false"
}

teardown() {
  common_teardown
}

@test "write-maven-build-summary writes key build details" {
  run_script "summary/write-maven-build-summary.sh"

  assert_success
  run get_summary
  assert_output --partial "Maven Build Summary"
  assert_output --partial 'se.digg:demo:1.2.3'
  assert_output --partial "Java:** 21"
}

@test "write-maven-build-summary reflects skipped tests" {
  export SKIP_TESTS="true"

  run_script "summary/write-maven-build-summary.sh"

  assert_success
  run get_summary
  assert_output --partial "⊘ Skipped"
}
