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
}

teardown() {
  common_teardown
}

@test "generate-artifact-names generates debug-name, release-name, aab-name, sbom-name outputs" {
  run_script "android/generate-artifact-names.sh" "false" "prefix" "myapp" "prod"

  assert_success
  assert_output --partial "debug-name="
  assert_output --partial "release-name="
  assert_output --partial "aab-name="
  assert_output --partial "sbom-name="
}

@test "generate-artifact-names includes repo name in all artifact names" {
  run_script "android/generate-artifact-names.sh" "false" "" "myapp" ""

  assert_success
  assert_line --partial "debug-name=myapp - APK debug"
  assert_line --partial "release-name=myapp - APK release"
  assert_line --partial "aab-name=myapp - AAB release"
  assert_line --partial "sbom-name=myapp - build SBOM"
}

@test "generate-artifact-names sbom-name includes flavor for matrix disambiguation" {
  # Matrix dispatch in release-build-stage.yml runs gradle-android per flavor;
  # without a flavor-scoped sbom-name, parallel uploads collide on a fixed name.
  run_script "android/generate-artifact-names.sh" "false" "" "myapp" "prod"
  assert_success
  assert_line --partial "sbom-name=myapp - prod - build SBOM"
}

@test "generate-artifact-names includes prefix when provided" {
  run_script "android/generate-artifact-names.sh" "false" "ci" "myapp" ""

  assert_success
  assert_line --partial "debug-name=ci - myapp - APK debug"
  assert_line --partial "release-name=ci - myapp - APK release"
  assert_line --partial "aab-name=ci - myapp - AAB release"
}

@test "generate-artifact-names includes flavor when provided" {
  run_script "android/generate-artifact-names.sh" "false" "" "myapp" "prod"

  assert_success
  assert_line --partial "debug-name=myapp - prod - APK debug"
  assert_line --partial "release-name=myapp - prod - APK release"
  assert_line --partial "aab-name=myapp - prod - AAB release"
}

@test "generate-artifact-names omits date stamp when include-date is false" {
  run_script "android/generate-artifact-names.sh" "false" "" "myapp" ""

  assert_success
  assert_line "debug-name=myapp - APK debug"
  assert_line "release-name=myapp - APK release"
  assert_line "aab-name=myapp - AAB release"
}

@test "generate-artifact-names includes date stamp when include-date is true" {
  run_script "android/generate-artifact-names.sh" "true" "" "myapp" ""

  assert_success
  assert_output --partial "APK debug"
  assert_output --partial "APK release"
  assert_output --partial "AAB release"
  # Date stamp is present (format YYYY-MM-DD)
  assert_output --regexp "[0-9]{4}-[0-9]{2}-[0-9]{2} - myapp"
}

@test "generate-artifact-names output format is key=value" {
  run_script "android/generate-artifact-names.sh" "false" "" "myapp" ""

  assert_success
  assert_line --regexp "^debug-name=.+"
  assert_line --regexp "^release-name=.+"
  assert_line --regexp "^aab-name=.+"
}

@test "generate-artifact-names combines prefix and flavor" {
  run_script "android/generate-artifact-names.sh" "false" "ci" "myapp" "staging"

  assert_success
  assert_line --partial "debug-name=ci - myapp - staging - APK debug"
  assert_line --partial "release-name=ci - myapp - staging - APK release"
  assert_line --partial "aab-name=ci - myapp - staging - AAB release"
}
