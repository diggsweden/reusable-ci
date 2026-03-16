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
  export JAVA_VERSION="21"
  export BUILD_MODULE="app"
  export GRADLE_TASKS="build publish"
  export SKIP_TESTS="false"
  export ENABLE_SIGNING="true"
  export VERSION="1.2.3"
  export VERSION_CODE="123"
}

teardown() {
  common_teardown
}

@test "write-gradle-build-summary writes key build details" {
  run_script "summary/write-gradle-build-summary.sh"

  assert_success
  run get_summary
  assert_output --partial "Gradle Build Summary"
  assert_output --partial "Java:** 21"
  assert_output --partial "Module:** app"
  assert_output --partial "Version:** 1.2.3 (123)"
}

@test "write-gradle-build-summary reflects skipped tests and disabled signing" {
  export SKIP_TESTS="true"
  export ENABLE_SIGNING="false"

  run_script "summary/write-gradle-build-summary.sh"

  assert_success
  run get_summary
  assert_output --partial "⊘ Skipped"
  assert_output --partial "⊘ Disabled"
}
