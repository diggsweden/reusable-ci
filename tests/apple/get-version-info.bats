#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/../test_helper.bash"

# =============================================================================
# Setup / Teardown
# =============================================================================

setup() {
  common_setup
  setup_github_env
  # Create mock .xcodeproj with project.pbxproj
  mkdir -p "${TEST_DIR}/MyApp.xcodeproj"
  cat > "${TEST_DIR}/MyApp.xcodeproj/project.pbxproj" <<'EOF'
/* Begin of pbxproj */
MARKETING_VERSION = 2.1.0;
CURRENT_PROJECT_VERSION = 15;
/* End */
EOF
}

teardown() {
  common_teardown
}

# =============================================================================
# Version Reading
# =============================================================================

@test "get-version-info reads version from project file" {
  pushd "$TEST_DIR" >/dev/null
  run_script "apple/get-version-info.sh" "MyApp.xcodeproj" ""
  popd >/dev/null

  assert_success
  assert_output --partial "version=2.1.0"
}

@test "get-version-info reads build number from project file" {
  pushd "$TEST_DIR" >/dev/null
  run_script "apple/get-version-info.sh" "MyApp.xcodeproj" ""
  popd >/dev/null

  assert_success
  assert_output --partial "build=15"
}

# =============================================================================
# Output Format
# =============================================================================

@test "get-version-info outputs version=X.Y.Z format" {
  pushd "$TEST_DIR" >/dev/null
  run_script "apple/get-version-info.sh" "MyApp.xcodeproj" ""
  popd >/dev/null

  assert_success
  assert_output --regexp "version=[0-9]+\.[0-9]+\.[0-9]+"
}

@test "get-version-info outputs build=N format" {
  pushd "$TEST_DIR" >/dev/null
  run_script "apple/get-version-info.sh" "MyApp.xcodeproj" ""
  popd >/dev/null

  assert_success
  assert_output --regexp "build=[0-9]+"
}

@test "get-version-info prints version info to stderr" {
  pushd "$TEST_DIR" >/dev/null
  run_script "apple/get-version-info.sh" "MyApp.xcodeproj" ""
  popd >/dev/null

  assert_success
  assert_stderr_contains "Version: 2.1.0 (15)"
}

# =============================================================================
# Missing Project File
# =============================================================================

@test "get-version-info falls back to unknown when project file not found" {
  pushd "$TEST_DIR" >/dev/null
  run_script "apple/get-version-info.sh" "NonExistent.xcodeproj" ""
  popd >/dev/null

  assert_success
  assert_output --partial "version=unknown"
  assert_output --partial "build=unknown"
}

@test "get-version-info shows warning when project file missing" {
  pushd "$TEST_DIR" >/dev/null
  run_script "apple/get-version-info.sh" "NonExistent.xcodeproj" ""
  popd >/dev/null

  assert_success
  assert_output --partial "::warning::Could not determine version from project file"
}

# =============================================================================
# Auto-Discovery
# =============================================================================

@test "get-version-info finds .xcodeproj automatically when no project arg given" {
  pushd "$TEST_DIR" >/dev/null
  run_script "apple/get-version-info.sh" "" ""
  popd >/dev/null

  assert_success
  assert_output --partial "version=2.1.0"
  assert_output --partial "build=15"
}
