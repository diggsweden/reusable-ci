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
  common_setup_with_isolated_git
}

teardown() {
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

run_validate_tag_uniqueness() {
  run_script "validation/validate-tag-uniqueness.sh" "$@"
}

# =============================================================================
# Input Validation Tests
# =============================================================================

@test "validate-tag-uniqueness.sh requires tag name argument" {
  run_validate_tag_uniqueness

  assert_failure
  # Usage message goes to stderr
  [[ "$stderr" == *"Usage"* ]]
}

@test "validate-tag-uniqueness.sh fails on empty tag name" {
  run_validate_tag_uniqueness ""

  assert_failure
  # Usage message goes to stderr
  [[ "$stderr" == *"Usage"* ]]
}

# =============================================================================
# Unique Tag Tests
# =============================================================================

@test "validate-tag-uniqueness.sh accepts unique tag" {
  git tag -a v1.0.0 -m "Release v1.0.0"

  run_validate_tag_uniqueness "v1.0.0"

  assert_success
  assert_output --partial "points to a unique commit"
}

@test "validate-tag-uniqueness.sh accepts unique lightweight tag" {
  git tag v1.0.0

  run_validate_tag_uniqueness "v1.0.0"

  assert_success
  assert_output --partial "unique commit"
}

@test "validate-tag-uniqueness.sh accepts tags on different commits" {
  git tag -a v1.0.0 -m "Release v1.0.0"
  add_commit "Second commit"
  git tag -a v1.0.1 -m "Release v1.0.1"

  run_validate_tag_uniqueness "v1.0.1"

  assert_success
  assert_output --partial "unique commit"
}

@test "validate-tag-uniqueness.sh accepts tag with many other unique tags" {
  git tag -a v1.0.0 -m "Release v1.0.0"
  add_commit "Second"
  git tag -a v1.0.1 -m "Release v1.0.1"
  add_commit "Third"
  git tag -a v1.0.2 -m "Release v1.0.2"
  add_commit "Fourth"
  git tag -a v1.0.3 -m "Release v1.0.3"

  run_validate_tag_uniqueness "v1.0.3"

  assert_success
  assert_output --partial "unique commit"
}

# =============================================================================
# Duplicate Tag Tests
# =============================================================================

@test "validate-tag-uniqueness.sh rejects duplicate tags on same commit" {
  git tag -a v1.0.0 -m "Release v1.0.0"
  git tag -a v1.0.1 -m "Release v1.0.1"

  run_validate_tag_uniqueness "v1.0.0"

  assert_failure
  assert_output --partial "same commit as other tag"
  assert_output --partial "v1.0.1"
}

@test "validate-tag-uniqueness.sh lists all conflicting tags" {
  git tag -a v1.0.0 -m "Release v1.0.0"
  git tag -a also-v1 -m "Also v1"
  git tag -a another -m "Another"

  run_validate_tag_uniqueness "v1.0.0"

  assert_failure
  assert_output --partial "also-v1"
  assert_output --partial "another"
}

@test "validate-tag-uniqueness.sh detects duplicate with lightweight tag" {
  git tag -a v1.0.0 -m "Release v1.0.0"
  git tag lightweight-tag

  run_validate_tag_uniqueness "v1.0.0"

  assert_failure
  assert_output --partial "lightweight-tag"
}

@test "validate-tag-uniqueness.sh detects duplicate from lightweight tag perspective" {
  git tag lightweight-tag
  git tag -a v1.0.0 -m "Release v1.0.0"

  run_validate_tag_uniqueness "lightweight-tag"

  assert_failure
  assert_output --partial "v1.0.0"
}

# =============================================================================
# Output Information Tests
# =============================================================================

@test "validate-tag-uniqueness.sh shows commit hash" {
  git tag -a v1.0.0 -m "Release v1.0.0"
  local commit
  commit=$(git rev-parse HEAD)

  run_validate_tag_uniqueness "v1.0.0"

  assert_success
  assert_output --partial "$commit"
}

@test "validate-tag-uniqueness.sh shows short commit hash" {
  git tag -a v1.0.0 -m "Release v1.0.0"
  local short_commit
  short_commit=$(git rev-parse --short HEAD)

  run_validate_tag_uniqueness "v1.0.0"

  assert_success
  assert_output --partial "$short_commit"
}

@test "validate-tag-uniqueness.sh mentions git-cliff limitation" {
  git tag -a v1.0.0 -m "Release v1.0.0"
  git tag -a duplicate -m "Duplicate tag"

  run_validate_tag_uniqueness "v1.0.0"

  assert_failure
  assert_output --partial "git-cliff"
  assert_output --partial "changelog"
}

# =============================================================================
# Edge Case Tests
# =============================================================================

@test "validate-tag-uniqueness.sh handles tag with special characters in message" {
  git tag -a v1.0.0 -m "Release with 'quotes' and \"double quotes\""

  run_validate_tag_uniqueness "v1.0.0"

  assert_success
  assert_output --partial "unique commit"
}

@test "validate-tag-uniqueness.sh handles tag starting with different prefix" {
  git tag -a release-1.0.0 -m "Release 1.0.0"

  run_validate_tag_uniqueness "release-1.0.0"

  assert_success
  assert_output --partial "unique commit"
}
