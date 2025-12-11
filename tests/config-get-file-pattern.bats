#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2155
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

# Run get-file-pattern with debug output
run_get_file_pattern() {
  run_script "config/get-file-pattern.sh" "$@"
}

# =============================================================================
# Input Validation Tests
# =============================================================================

@test "get-file-pattern.sh requires project-type argument" {
  run_get_file_pattern

  assert_failure
  assert_stderr_contains "Error: PROJECT_TYPE is required"
}

@test "get-file-pattern.sh fails on empty project-type" {
  run_get_file_pattern ""

  assert_failure
}

# =============================================================================
# Maven Project Type Tests
# =============================================================================

@test "get-file-pattern.sh returns maven pattern" {
  run_get_file_pattern "maven"

  assert_success
  assert_output --partial "CHANGELOG.md"
  assert_output --partial "pom.xml"
}

@test "get-file-pattern.sh includes glob pattern for maven" {
  run_get_file_pattern "maven"

  assert_success
  assert_output --partial ":(glob)**/pom.xml"
}

# =============================================================================
# NPM Project Type Tests
# =============================================================================

@test "get-file-pattern.sh returns npm pattern" {
  run_get_file_pattern "npm"

  assert_success
  assert_output --partial "CHANGELOG.md"
  assert_output --partial "package.json"
  assert_output --partial "package-lock.json"
}

@test "get-file-pattern.sh npm pattern includes lock file" {
  run_get_file_pattern "npm"

  assert_success
  assert_output --partial "package-lock.json"
}

# =============================================================================
# Gradle Project Type Tests
# =============================================================================

@test "get-file-pattern.sh returns gradle pattern" {
  run_get_file_pattern "gradle"

  assert_success
  assert_output --partial "CHANGELOG.md"
  assert_output --partial "gradle.properties"
}

@test "get-file-pattern.sh gradle includes kotlin build files" {
  run_get_file_pattern "gradle"

  assert_success
  assert_output --partial "build.gradle.kts"
  assert_output --partial "settings.gradle.kts"
}

@test "get-file-pattern.sh gradle includes groovy build files" {
  run_get_file_pattern "gradle"

  assert_success
  assert_output --partial "build.gradle"
  assert_output --partial "settings.gradle"
}

# =============================================================================
# Gradle Android Project Type Tests
# =============================================================================

@test "get-file-pattern.sh returns gradle-android pattern" {
  run_get_file_pattern "gradle-android"

  assert_success
  assert_output --partial "CHANGELOG.md"
  assert_output --partial "gradle.properties"
}

@test "get-file-pattern.sh gradle-android same as gradle" {
  local gradle_output android_output

  run_get_file_pattern "gradle"
  gradle_output="$output"

  run_get_file_pattern "gradle-android"
  android_output="$output"

  assert_equal "$gradle_output" "$android_output"
}

# =============================================================================
# Xcode iOS Project Type Tests
# =============================================================================

@test "get-file-pattern.sh returns xcode-ios pattern" {
  run_get_file_pattern "xcode-ios"

  assert_success
  assert_output --partial "CHANGELOG.md"
  assert_output --partial "versions.xcconfig"
}

@test "get-file-pattern.sh xcode-ios includes xcconfig glob" {
  run_get_file_pattern "xcode-ios"

  assert_success
  assert_output --partial ":(glob)**/*.xcconfig"
}

# =============================================================================
# Python Project Type Tests
# =============================================================================

@test "get-file-pattern.sh returns python pattern" {
  run_get_file_pattern "python"

  assert_success
  assert_output --partial "CHANGELOG.md"
  assert_output --partial "pyproject.toml"
}

# =============================================================================
# Go Project Type Tests
# =============================================================================

@test "get-file-pattern.sh returns go pattern" {
  run_get_file_pattern "go"

  assert_success
  assert_output --partial "CHANGELOG.md"
  assert_output --partial "go.mod"
}

# =============================================================================
# Rust Project Type Tests
# =============================================================================

@test "get-file-pattern.sh returns rust pattern" {
  run_get_file_pattern "rust"

  assert_success
  assert_output --partial "CHANGELOG.md"
  assert_output --partial "Cargo.toml"
  assert_output --partial "Cargo.lock"
}

# =============================================================================
# Unknown Project Type Tests
# =============================================================================

@test "get-file-pattern.sh returns CHANGELOG.md for unknown type" {
  run_get_file_pattern "unknown-type"

  assert_success
  assert_output "CHANGELOG.md"
}

@test "get-file-pattern.sh returns CHANGELOG.md for meta type" {
  run_get_file_pattern "meta"

  assert_success
  assert_output "CHANGELOG.md"
}

# =============================================================================
# Custom Pattern Override Tests
# =============================================================================

@test "get-file-pattern.sh uses custom pattern when provided" {
  run_get_file_pattern "maven" "custom-file.txt other-file.md"

  assert_success
  assert_output "custom-file.txt other-file.md"
}

@test "get-file-pattern.sh custom pattern overrides default" {
  run_get_file_pattern "npm" "my-custom-pattern.json"

  assert_success
  assert_output "my-custom-pattern.json"
  refute_output --partial "package.json"
}

@test "get-file-pattern.sh custom pattern with glob" {
  run_get_file_pattern "maven" ":(glob)**/*.xml CHANGELOG.md"

  assert_success
  assert_output ":(glob)**/*.xml CHANGELOG.md"
}

@test "get-file-pattern.sh empty custom pattern uses default" {
  run_get_file_pattern "maven" ""

  assert_success
  assert_output --partial "pom.xml"
}

# =============================================================================
# Output Format Tests
# =============================================================================

@test "get-file-pattern.sh outputs single line" {
  run_get_file_pattern "maven"

  assert_success
  # Output should be usable in git commands (space-separated on one line)
  local line_count
  line_count=$(echo "$output" | wc -l)
  assert [ "$line_count" -le 2 ]  # May have trailing newline
}

@test "get-file-pattern.sh all types include CHANGELOG.md" {
  local types=("maven" "npm" "gradle" "gradle-android" "xcode-ios" "python" "go" "rust")

  for type in "${types[@]}"; do
    run_get_file_pattern "$type"
    assert_success
    assert_output --partial "CHANGELOG.md"
  done
}
