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
  common_setup
  export JAVA_VERSION="17"
  export JDK_DIST="temurin"
  export BUILD_MODULE="app"
  export FLAVOR="prod"
  export BUILD_TYPES="debug,release"
  export INCLUDE_AAB="true"
  export SIGNING="false"
  export SKIP_TESTS="false"
  export VERSION="1.0.0"
  export VERSION_CODE="42"
  export DEBUG_NAME="debug-name"
  export RELEASE_NAME="release-name"
  export AAB_NAME="aab-name"
}

teardown() {
  common_teardown
}

@test "write-android-build-summary shows java version and JDK dist" {
  run_script "summary/write-android-build-summary.sh"

  assert_success
  assert_output --partial "17"
  assert_output --partial "temurin"
}

@test "write-android-build-summary shows build module" {
  run_script "summary/write-android-build-summary.sh"

  assert_success
  assert_output --partial "app"
}

@test "write-android-build-summary shows flavor when provided" {
  run_script "summary/write-android-build-summary.sh"

  assert_success
  assert_output --partial "prod"
}

@test "write-android-build-summary shows default flavor when empty" {
  export FLAVOR=""

  run_script "summary/write-android-build-summary.sh"

  assert_success
  assert_output --partial "default"
}

@test "write-android-build-summary shows debug APK when build types contain debug" {
  export DEBUG_NAME="my-debug"
  export RELEASE_NAME="my-release"
  export AAB_NAME="my-aab"

  run_script "summary/write-android-build-summary.sh"

  assert_success
  assert_output --partial "Debug APK"
  assert_output --partial "my-debug"
}

@test "write-android-build-summary shows release APK when build types contain release" {
  export RELEASE_NAME="my-release"

  run_script "summary/write-android-build-summary.sh"

  assert_success
  assert_output --partial "Release APK"
  assert_output --partial "my-release"
}

@test "write-android-build-summary shows AAB when include-aab is true and release in build types" {
  export AAB_NAME="my-aab"

  run_script "summary/write-android-build-summary.sh"

  assert_success
  assert_output --partial "Release AAB"
  assert_output --partial "my-aab"
}

@test "write-android-build-summary shows tests skipped when SKIP_TESTS is true" {
  export SKIP_TESTS="true"

  run_script "summary/write-android-build-summary.sh"

  assert_success
  assert_output --partial "Skipped"
}

@test "write-android-build-summary shows signing disabled" {
  run_script "summary/write-android-build-summary.sh"

  assert_success
  assert_output --partial "Disabled"
}
