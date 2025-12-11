#!/usr/bin/env bats

# shellcheck disable=SC1083,SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
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
  init_remote_repo
}

teardown() {
  cleanup_remote
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

# Run validate-tag-commit with debug output
run_validate_tag_commit() {
  run_script "validation/validate-tag-commit.sh" "$@"
}

# Push changes to remote
push_to_remote() {
  git push -q origin main 2>/dev/null || git push -q origin master 2>/dev/null || true
}

# =============================================================================
# Input Validation Tests
# =============================================================================

@test "validate-tag-commit.sh shows usage on empty input" {
  run_validate_tag_commit ""

  assert_failure
  assert_output --partial "Usage"
}

@test "validate-tag-commit.sh shows usage with no arguments" {
  run_validate_tag_commit

  assert_failure
  assert_output --partial "Usage"
}

# =============================================================================
# Tag Commit Information Tests
# =============================================================================

@test "validate-tag-commit.sh shows tag commit hash" {
  git tag -a v1.0.0 -m "Release"
  local expected_commit
  expected_commit=$(git rev-parse HEAD)

  run_validate_tag_commit "v1.0.0" "main"

  assert_output --partial "$expected_commit"
  assert_output --partial "points to commit"
}

@test "validate-tag-commit.sh shows short commit hash" {
  git tag -a v1.0.0 -m "Release"
  local short_commit
  short_commit=$(git rev-parse --short HEAD)

  run_validate_tag_commit "v1.0.0" "main"

  assert_output --partial "$short_commit"
}

# =============================================================================
# Branch HEAD Validation Tests
# =============================================================================

@test "validate-tag-commit.sh validates tag on branch HEAD" {
  git tag -a v1.0.0 -m "Release"
  push_to_remote

  run_validate_tag_commit "v1.0.0" "main"

  assert_output --partial "points to commit"
}

@test "validate-tag-commit.sh uses main as default branch" {
  git tag -a v1.0.0 -m "Release"

  run_validate_tag_commit "v1.0.0"

  assert_output --partial "main"
}

@test "validate-tag-commit.sh shows branch HEAD info" {
  git tag -a v1.0.0 -m "Release"

  run_validate_tag_commit "v1.0.0" "main"

  assert_output --partial "Branch"
  assert_output --partial "HEAD"
}

# =============================================================================
# Different Branch Tests
# =============================================================================

@test "validate-tag-commit.sh works with custom branch name" {
  git checkout -q -b develop
  add_commit "Develop commit"
  git tag -a v1.0.0 -m "Release"

  run_validate_tag_commit "v1.0.0" "develop"

  assert_output --partial "develop"
  assert_output --partial "points to commit"
}

@test "validate-tag-commit.sh handles release branch" {
  git checkout -q -b release/1.0
  add_commit "Release commit"
  git tag -a v1.0.0 -m "Release"

  run_validate_tag_commit "v1.0.0" "release/1.0"

  assert_output --partial "points to commit"
}

# =============================================================================
# Tag Type Tests
# =============================================================================

@test "validate-tag-commit.sh works with lightweight tag" {
  git tag v1.0.0

  run_validate_tag_commit "v1.0.0" "main"

  assert_output --partial "points to commit"
}

@test "validate-tag-commit.sh works with annotated tag" {
  git tag -a v1.0.0 -m "Release v1.0.0"

  run_validate_tag_commit "v1.0.0" "main"

  assert_output --partial "points to commit"
}

# =============================================================================
# Edge Case Tests
# =============================================================================

@test "validate-tag-commit.sh handles tag not on HEAD" {
  git tag -a v1.0.0 -m "Release"
  add_commit "After tag"

  run_validate_tag_commit "v1.0.0" "main"

  # Should still show tag info even if not on HEAD
  assert_output --partial "points to commit"
}

@test "validate-tag-commit.sh shows tag commit when HEAD is different" {
  git tag -a v1.0.0 -m "Release"
  local tag_commit
  tag_commit=$(git rev-parse v1.0.0^{commit})
  add_commit "After tag"
  push_to_remote

  run_validate_tag_commit "v1.0.0" "main"

  # Output should include the tag commit
  assert_output --partial "$tag_commit"
}

@test "validate-tag-commit.sh handles multiple tags with remote" {
  git tag -a v0.9.0 -m "Pre-release"
  add_commit "Final fixes"
  git tag -a v1.0.0 -m "Release"
  push_to_remote
  git push -q origin v1.0.0 2>/dev/null || true

  run_validate_tag_commit "v1.0.0" "main"

  assert_output --partial "points to commit"
}
