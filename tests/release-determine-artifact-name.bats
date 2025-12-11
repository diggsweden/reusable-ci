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

run_determine_artifact_name() {
  run_script "release/determine-artifact-name.sh" "$@"
}

# =============================================================================
# Input Validation Tests
# =============================================================================

@test "determine-artifact-name.sh requires project-type argument" {
  run_determine_artifact_name

  assert_failure
  assert_stderr_contains "Usage"
}

# =============================================================================
# Maven Project Tests
# =============================================================================

@test "determine-artifact-name.sh returns maven-build-artifacts for maven" {
  run_determine_artifact_name "maven"

  assert_success
  assert_output "name=maven-build-artifacts"
}

# =============================================================================
# NPM Project Tests
# =============================================================================

@test "determine-artifact-name.sh returns npm-build-artifacts for npm" {
  run_determine_artifact_name "npm"

  assert_success
  assert_output "name=npm-build-artifacts"
}

# =============================================================================
# Gradle Project Tests
# =============================================================================

@test "determine-artifact-name.sh returns gradle-build-artifacts for gradle" {
  run_determine_artifact_name "gradle"

  assert_success
  assert_output "name=gradle-build-artifacts"
}

# =============================================================================
# Python Project Tests
# =============================================================================

@test "determine-artifact-name.sh returns python-build-artifacts for python" {
  run_determine_artifact_name "python"

  assert_success
  assert_output "name=python-build-artifacts"
}

# =============================================================================
# Go Project Tests
# =============================================================================

@test "determine-artifact-name.sh returns go-build-artifacts for go" {
  run_determine_artifact_name "go"

  assert_success
  assert_output "name=go-build-artifacts"
}

# =============================================================================
# Rust Project Tests
# =============================================================================

@test "determine-artifact-name.sh returns rust-build-artifacts for rust" {
  run_determine_artifact_name "rust"

  assert_success
  assert_output "name=rust-build-artifacts"
}

# =============================================================================
# Unknown Project Type Tests
# =============================================================================

@test "determine-artifact-name.sh returns generic name for unknown type" {
  run_determine_artifact_name "unknown"

  assert_success
  assert_output "name=build-artifacts"
}

@test "determine-artifact-name.sh handles empty-ish types as unknown" {
  run_determine_artifact_name "other"

  assert_success
  assert_output "name=build-artifacts"
}

# =============================================================================
# Output Format Tests
# =============================================================================

@test "determine-artifact-name.sh output is in name=value format" {
  run_determine_artifact_name "maven"

  assert_success
  assert_output --regexp "^name=.+$"
}

@test "determine-artifact-name.sh outputs single line" {
  run_determine_artifact_name "maven"

  assert_success
  local line_count
  line_count=$(echo "$output" | wc -l)
  [[ "$line_count" -eq 1 ]]
}

# =============================================================================
# Case Sensitivity Tests
# =============================================================================

@test "determine-artifact-name.sh is case sensitive - Maven not recognized" {
  run_determine_artifact_name "Maven"

  assert_success
  assert_output "name=build-artifacts"
}

@test "determine-artifact-name.sh is case sensitive - NPM not recognized" {
  run_determine_artifact_name "NPM"

  assert_success
  assert_output "name=build-artifacts"
}
