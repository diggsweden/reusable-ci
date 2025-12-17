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

run_determine_build_tasks() {
  local flavor="${1:-}"
  local build_types="${2:-debug,release}"
  local include_aab="${3:-true}"
  local build_module="${4:-app}"
  
  run --separate-stderr bash "$SCRIPTS_DIR/android/determine-build-tasks.sh" \
    "$flavor" "$build_types" "$include_aab" "$build_module"
  debug_output
}

# =============================================================================
# Determine Build Tasks Tests
# =============================================================================

@test "determine-build-tasks.sh generates default tasks without flavor" {
  run_determine_build_tasks "" "debug,release" "true" "app"

  assert_success
  assert_output --partial "tasks=build assembleDebug assembleRelease app:bundleRelease"
}

@test "determine-build-tasks.sh generates tasks with flavor" {
  run_determine_build_tasks "demo" "debug,release" "true" "app"

  assert_success
  assert_output --partial "tasks=build assembleDemoDebug assembleDemoRelease app:bundleDemoRelease"
}

@test "determine-build-tasks.sh capitalizes flavor correctly" {
  run_determine_build_tasks "DEMO" "release" "true" "app"

  assert_success
  # Should capitalize first letter only: Demo not DEMO
  assert_output --partial "assembleDemoRelease"
  assert_output --partial "bundleDemoRelease"
}

@test "determine-build-tasks.sh generates debug only tasks" {
  run_determine_build_tasks "demo" "debug" "true" "app"

  assert_success
  assert_output --partial "assembleDemoDebug"
  refute_output --partial "assembleDemoRelease"
  refute_output --partial "bundleDemoRelease"
}

@test "determine-build-tasks.sh generates release only tasks" {
  run_determine_build_tasks "demo" "release" "true" "app"

  assert_success
  assert_output --partial "assembleDemoRelease"
  assert_output --partial "bundleDemoRelease"
  refute_output --partial "assembleDemoDebug"
}

@test "determine-build-tasks.sh excludes AAB when include_aab is false" {
  run_determine_build_tasks "demo" "release" "false" "app"

  assert_success
  assert_output --partial "assembleDemoRelease"
  refute_output --partial "bundleDemoRelease"
}

@test "determine-build-tasks.sh uses custom build module for AAB" {
  run_determine_build_tasks "demo" "release" "true" "mymodule"

  assert_success
  assert_output --partial "mymodule:bundleDemoRelease"
}

@test "determine-build-tasks.sh outputs build info to stderr" {
  run_determine_build_tasks "demo" "release" "true" "app"

  assert_success
  assert_equal "$stderr" "Building with tasks: build assembleDemoRelease app:bundleDemoRelease"
}

@test "determine-build-tasks.sh handles prod flavor" {
  run_determine_build_tasks "prod" "release" "true" "app"

  assert_success
  assert_output --partial "assembleProdRelease"
  assert_output --partial "bundleProdRelease"
}
