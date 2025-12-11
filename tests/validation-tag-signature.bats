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
  common_setup_with_isolated_git
}

teardown() {
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

run_validate_tag_signature() {
  run_script "validation/validate-tag-signature.sh" "$@"
}

# =============================================================================
# Input Validation Tests
# =============================================================================

@test "validate-tag-signature.sh shows usage on empty input" {
  run_validate_tag_signature ""

  assert_failure
  assert_output --partial "Usage"
}

@test "validate-tag-signature.sh shows usage with no arguments" {
  run_validate_tag_signature

  assert_failure
  assert_output --partial "Usage"
}

# =============================================================================
# Lightweight Tag Tests
# =============================================================================

@test "validate-tag-signature.sh rejects lightweight tag" {
  git tag v1.0.0

  run_validate_tag_signature "v1.0.0" "test/repo"

  assert_failure
  assert_output --partial "lightweight tag"
  assert_output --partial "not annotated"
}

@test "validate-tag-signature.sh shows example command on lightweight tag failure" {
  git tag v1.0.0

  run_validate_tag_signature "v1.0.0" "test/repo"

  assert_failure
  assert_output --partial "git tag -a"
}

@test "validate-tag-signature.sh explains lightweight tag issue" {
  git tag v1.0.0

  run_validate_tag_signature "v1.0.0" "test/repo"

  assert_failure
  assert_output --partial "lightweight"
}

# =============================================================================
# Annotated Tag Tests (Unsigned)
# =============================================================================

@test "validate-tag-signature.sh rejects unsigned annotated tag" {
  git tag -a v1.0.0 -m "Release v1.0.0"

  run_validate_tag_signature "v1.0.0" "test/repo"

  assert_failure
  assert_output --partial "not signed"
}

@test "validate-tag-signature.sh detects annotated tag type" {
  git tag -a v1.0.0 -m "Release v1.0.0"

  run_validate_tag_signature "v1.0.0" "test/repo"

  assert_output --partial "is annotated"
  assert_output --partial "object type: tag"
}

@test "validate-tag-signature.sh shows signing help on unsigned tag" {
  git tag -a v1.0.0 -m "Release v1.0.0"

  run_validate_tag_signature "v1.0.0" "test/repo"

  assert_failure
  assert_output --partial "git tag -s"
  assert_output --partial "cryptographically signed"
}

# =============================================================================
# Error Help Tests
# =============================================================================

@test "validate-tag-signature.sh shows documentation link on failure" {
  git tag v1.0.0

  run_validate_tag_signature "v1.0.0" "test/repo"

  assert_failure
  assert_output --partial "WORKFLOWS.md"
}

@test "validate-tag-signature.sh shows tag details in output" {
  git tag -a v1.0.0 -m "Release"

  run_validate_tag_signature "v1.0.0" "myorg/myrepo"

  assert_output --partial "v1.0.0"
  assert_output --partial "not signed"
}

# =============================================================================
# Tag Information Tests
# =============================================================================

@test "validate-tag-signature.sh shows tag name being validated" {
  git tag -a v1.0.0 -m "Release"

  run_validate_tag_signature "v1.0.0" "test/repo"

  assert_output --partial "v1.0.0"
}

@test "validate-tag-signature.sh shows validation header" {
  git tag -a v1.0.0 -m "Release"

  run_validate_tag_signature "v1.0.0" "test/repo"

  assert_output --partial "Validating"
}

# =============================================================================
# Edge Case Tests
# =============================================================================

@test "validate-tag-signature.sh handles tag with long message" {
  git tag -a v1.0.0 -m "This is a very long release message that spans multiple words and contains detailed release notes"

  run_validate_tag_signature "v1.0.0" "test/repo"

  assert_failure
  assert_output --partial "not signed"
}

@test "validate-tag-signature.sh handles tag with special characters in name" {
  git tag -a v1.0.0-rc.1 -m "Release candidate 1"

  run_validate_tag_signature "v1.0.0-rc.1" "test/repo"

  assert_failure
  assert_output --partial "not signed"
}

@test "validate-tag-signature.sh handles tag with prerelease suffix" {
  git tag -a v1.0.0-SNAPSHOT -m "Snapshot release"

  run_validate_tag_signature "v1.0.0-SNAPSHOT" "test/repo"

  assert_failure
  assert_output --partial "not signed"
}

# =============================================================================
# Security Information Tests
# =============================================================================

@test "validate-tag-signature.sh explains importance of signing" {
  git tag -a v1.0.0 -m "Release"

  run_validate_tag_signature "v1.0.0" "test/repo"

  assert_failure
  assert_output --partial "signed"
}

@test "validate-tag-signature.sh mentions GPG or SSH signing" {
  git tag -a v1.0.0 -m "Release"

  run_validate_tag_signature "v1.0.0" "test/repo"

  assert_failure
  assert_output --partial "git tag -s"
}
