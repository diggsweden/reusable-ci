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
  export GRADLE_TASKS="build publish"
  export SKIP_TESTS="false"
  export VERSION="1.2.3"
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
  assert_output --partial "Tasks:** build publish"
  assert_output --partial "Version:** 1.2.3"
}

@test "write-gradle-build-summary reflects skipped tests" {
  export SKIP_TESTS="true"

  run_script "summary/write-gradle-build-summary.sh"

  assert_success
  run get_summary
  assert_output --partial "⊘ Skipped"
}

@test "write-gradle-build-summary omits version line when VERSION is empty" {
  # JVM projects may carry version outside gradle.properties; absence
  # is a warning in the workflow, not a hard failure, and the summary
  # should simply skip the version line.
  unset VERSION

  run_script "summary/write-gradle-build-summary.sh"

  assert_success
  run get_summary
  refute_output --partial "Version:"
}

@test "write-gradle-build-summary errors without JAVA_VERSION" {
  unset JAVA_VERSION

  run_script "summary/write-gradle-build-summary.sh"

  assert_failure
}
