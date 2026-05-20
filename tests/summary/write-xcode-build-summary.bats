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
  export XCODE_VERSION="15.2"
  export SCHEME="MyApp"
  export CONFIGURATION="Release"
  export DESTINATION="generic/platform=iOS"
  export SIGNING="true"
  export VERSION="1.0.0"
  export BUILD_NUMBER="42"
  export IPA_NAME="MyApp-ipa"
}

teardown() {
  common_teardown
}

@test "write-xcode-build-summary shows Xcode version" {
  run_script "summary/write-xcode-build-summary.sh"

  assert_success
  assert_output --partial "15.2"
}

@test "write-xcode-build-summary shows scheme" {
  run_script "summary/write-xcode-build-summary.sh"

  assert_success
  assert_output --partial "MyApp"
}

@test "write-xcode-build-summary shows configuration" {
  run_script "summary/write-xcode-build-summary.sh"

  assert_success
  assert_output --partial "Release"
}

@test "write-xcode-build-summary shows destination" {
  run_script "summary/write-xcode-build-summary.sh"

  assert_success
  assert_output --partial "generic/platform=iOS"
}

@test "write-xcode-build-summary shows signing enabled when true" {
  run_script "summary/write-xcode-build-summary.sh"

  assert_success
  assert_output --partial "Enabled"
}

@test "write-xcode-build-summary shows IPA artifact name when signing enabled" {
  run_script "summary/write-xcode-build-summary.sh"

  assert_success
  assert_output --partial "IPA"
  assert_output --partial "MyApp-ipa"
}

@test "write-xcode-build-summary shows archive artifact when signing disabled" {
  export SIGNING="false"

  run_script "summary/write-xcode-build-summary.sh"

  assert_success
  assert_output --partial "Archive"
  assert_output --partial "MyApp-ipa-archive"
}

@test "write-xcode-build-summary shows version and build number" {
  run_script "summary/write-xcode-build-summary.sh"

  assert_success
  assert_output --partial "1.0.0"
  assert_output --partial "42"
}
