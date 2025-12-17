#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/test_helper.bash"

# =============================================================================
# Setup / Teardown
# =============================================================================

setup() {
  common_setup
}

teardown() {
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

run_get_version_info() {
  run --separate-stderr bash "$SCRIPTS_DIR/android/get-version-info.sh"
  debug_output
}

create_gradle_properties() {
  local version_name="${1:-1.0.0}"
  local version_code="${2:-1}"
  
  cat > "$TEST_DIR/gradle.properties" << EOF
versionName=${version_name}
versionCode=${version_code}
EOF
}

# =============================================================================
# Get Version Info Tests
# =============================================================================

@test "get-version-info.sh outputs warning when gradle.properties not found" {
  run_get_version_info

  assert_success
  assert_output --partial "version=unknown"
  assert_output --partial "version-code=unknown"
  assert_equal "$stderr" "::warning::gradle.properties not found, version info unavailable"
}

@test "get-version-info.sh parses version from gradle.properties" {
  create_gradle_properties "2.5.0" "42"

  run_get_version_info

  assert_success
  assert_output --partial "version=2.5.0"
  assert_output --partial "version-code=42"
}

@test "get-version-info.sh outputs version info to stderr" {
  create_gradle_properties "1.2.3" "10"

  run_get_version_info

  assert_success
  assert_equal "$stderr" "Version: 1.2.3 (10)"
}

@test "get-version-info.sh handles version with suffix" {
  create_gradle_properties "1.0.0-beta1" "5"

  run_get_version_info

  assert_success
  assert_output --partial "version=1.0.0-beta1"
  assert_output --partial "version-code=5"
}
