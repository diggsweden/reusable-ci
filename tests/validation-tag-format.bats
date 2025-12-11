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

# Run validate-tag-format with debug output
run_validate_tag_format() {
  run_script "validation/validate-tag-format.sh" "$@"
}

# =============================================================================
# Input Validation Tests
# =============================================================================

@test "validate-tag-format.sh rejects empty input" {
  run_validate_tag_format ""

  assert_failure
  assert_output --partial "Usage"
}

@test "validate-tag-format.sh rejects no arguments" {
  run_validate_tag_format

  assert_failure
  assert_output --partial "Usage"
}

# =============================================================================
# Valid Stable Version Tests
# =============================================================================

@test "validate-tag-format.sh accepts v1.0.0" {
  run_validate_tag_format "v1.0.0"

  assert_success
  assert_output --partial "Valid semantic version"
  assert_output --partial "Stable release"
}

@test "validate-tag-format.sh accepts v0.0.1" {
  run_validate_tag_format "v0.0.1"

  assert_success
  assert_output --partial "Valid semantic version"
}

@test "validate-tag-format.sh accepts v10.20.30" {
  run_validate_tag_format "v10.20.30"

  assert_success
  assert_output --partial "Version: 10.20.30"
}

@test "validate-tag-format.sh accepts v0.1.0" {
  run_validate_tag_format "v0.1.0"

  assert_success
  assert_output --partial "Valid semantic version"
}

@test "validate-tag-format.sh accepts v100.200.300" {
  run_validate_tag_format "v100.200.300"

  assert_success
  assert_output --partial "Valid semantic version"
}

# =============================================================================
# Valid Pre-release Version Tests
# =============================================================================

@test "validate-tag-format.sh accepts v2.3.4-beta.1" {
  run_validate_tag_format "v2.3.4-beta.1"

  assert_success
  assert_output --partial "Pre-release: beta.1"
  assert_output --partial "follows convention"
}

@test "validate-tag-format.sh accepts v1.0.0-alpha" {
  run_validate_tag_format "v1.0.0-alpha"

  assert_success
  assert_output --partial "Pre-release: alpha"
}

@test "validate-tag-format.sh accepts v1.0.0-rc.2" {
  run_validate_tag_format "v1.0.0-rc.2"

  assert_success
  assert_output --partial "Pre-release: rc.2"
}

@test "validate-tag-format.sh accepts v1.0.0-SNAPSHOT" {
  run_validate_tag_format "v1.0.0-SNAPSHOT"

  assert_success
  assert_output --partial "Pre-release: SNAPSHOT"
}

@test "validate-tag-format.sh accepts v1.0.0-dev" {
  run_validate_tag_format "v1.0.0-dev"

  assert_success
  assert_output --partial "Pre-release: dev"
}

@test "validate-tag-format.sh accepts v1.0.0-alpha.1" {
  run_validate_tag_format "v1.0.0-alpha.1"

  assert_success
  assert_output --partial "Pre-release: alpha.1"
}

@test "validate-tag-format.sh accepts v1.0.0-beta" {
  run_validate_tag_format "v1.0.0-beta"

  assert_success
  assert_output --partial "Pre-release: beta"
}

@test "validate-tag-format.sh accepts v1.0.0-rc.10" {
  run_validate_tag_format "v1.0.0-rc.10"

  assert_success
  assert_output --partial "Pre-release: rc.10"
}

# =============================================================================
# Non-standard Pre-release Warning Tests
# =============================================================================

@test "validate-tag-format.sh warns on non-standard prerelease" {
  run_validate_tag_format "v1.0.0-custom.123"

  assert_success
  assert_output --partial "Non-standard pre-release identifier"
  assert_output --partial "informational only"
}

@test "validate-tag-format.sh warns on unknown prerelease prefix" {
  run_validate_tag_format "v1.0.0-preview.1"

  assert_success
  assert_output --partial "Non-standard"
}

# =============================================================================
# Invalid Format Tests - Missing v Prefix
# =============================================================================

@test "validate-tag-format.sh rejects missing v prefix" {
  run_validate_tag_format "1.0.0"

  assert_failure
  assert_output --partial "Invalid tag format"
}

@test "validate-tag-format.sh rejects uppercase V prefix" {
  run_validate_tag_format "V1.0.0"

  assert_failure
  assert_output --partial "Invalid tag format"
}

# =============================================================================
# Invalid Format Tests - Incomplete Version
# =============================================================================

@test "validate-tag-format.sh rejects incomplete version" {
  run_validate_tag_format "v1.0"

  assert_failure
  assert_output --partial "Invalid tag format"
}

@test "validate-tag-format.sh rejects single digit version" {
  run_validate_tag_format "v1"

  assert_failure
  assert_output --partial "Invalid tag format"
}

@test "validate-tag-format.sh rejects version with four parts" {
  run_validate_tag_format "v1.0.0.0"

  assert_failure
  assert_output --partial "Invalid tag format"
}

# =============================================================================
# Invalid Format Tests - Non-numeric
# =============================================================================

@test "validate-tag-format.sh rejects non-numeric version" {
  run_validate_tag_format "vX.Y.Z"

  assert_failure
  assert_output --partial "Invalid tag format"
}

@test "validate-tag-format.sh rejects mixed alphanumeric major" {
  run_validate_tag_format "v1a.0.0"

  assert_failure
  assert_output --partial "Invalid tag format"
}

# =============================================================================
# Invalid Format Tests - Wrong Prefix
# =============================================================================

@test "validate-tag-format.sh rejects release prefix" {
  run_validate_tag_format "release-1.0.0"

  assert_failure
  assert_output --partial "Invalid tag format"
}

@test "validate-tag-format.sh rejects version prefix" {
  run_validate_tag_format "version-1.0.0"

  assert_failure
  assert_output --partial "Invalid tag format"
}

# =============================================================================
# Invalid Format Tests - Random Strings
# =============================================================================

@test "validate-tag-format.sh rejects random string" {
  run_validate_tag_format "foobar"

  assert_failure
  assert_output --partial "Invalid tag format"
}

@test "validate-tag-format.sh rejects commit-like string" {
  run_validate_tag_format "abc123def"

  assert_failure
  assert_output --partial "Invalid tag format"
}

# =============================================================================
# Leading Zeros Tests (Note: script currently accepts these)
# =============================================================================

@test "validate-tag-format.sh accepts leading zeros in major" {
  # Note: Strict semver rejects leading zeros, but this script accepts them
  run_validate_tag_format "v01.0.0"

  assert_success
  assert_output --partial "Valid semantic version"
}

@test "validate-tag-format.sh accepts leading zeros in minor" {
  # Note: Strict semver rejects leading zeros, but this script accepts them
  run_validate_tag_format "v1.01.0"

  assert_success
  assert_output --partial "Valid semantic version"
}

@test "validate-tag-format.sh accepts leading zeros in patch" {
  # Note: Strict semver rejects leading zeros, but this script accepts them
  run_validate_tag_format "v1.0.01"

  assert_success
  assert_output --partial "Valid semantic version"
}

# =============================================================================
# Help Message Tests
# =============================================================================

@test "validate-tag-format.sh shows semver help on failure" {
  run_validate_tag_format "bad-tag"

  assert_failure
  assert_output --partial "semver.org"
  assert_output --partial "vMAJOR.MINOR.PATCH"
}

@test "validate-tag-format.sh shows valid examples on failure" {
  run_validate_tag_format "invalid"

  assert_failure
  assert_output --partial "v1.0.0"
}

# =============================================================================
# Output Information Tests
# =============================================================================

@test "validate-tag-format.sh displays version components" {
  run_validate_tag_format "v2.5.10"

  assert_success
  assert_output --partial "Version: 2.5.10"
}

@test "validate-tag-format.sh identifies stable release" {
  run_validate_tag_format "v1.0.0"

  assert_success
  assert_output --partial "Stable release"
}

@test "validate-tag-format.sh identifies pre-release" {
  run_validate_tag_format "v1.0.0-alpha"

  assert_success
  assert_output --partial "Pre-release"
}
