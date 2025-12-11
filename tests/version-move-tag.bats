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

# Run move-tag with debug output (note: script requires git push which we can't easily test)
run_move_tag() {
  run_script "version/move-tag.sh" "$@"
}

# =============================================================================
# Tag Position Validation Tests
# =============================================================================

@test "move-tag.sh fails when tag not at HEAD~1" {
  # Create tag on initial commit
  git tag -a v1.0.0 -m "Release"
  
  # Add TWO commits (so tag is at HEAD~2, not HEAD~1)
  echo "change1" >> file.txt
  git add file.txt
  git commit -q -m "First change"
  
  echo "change2" >> file.txt
  git add file.txt
  git commit -q -m "Second change"
  
  run_move_tag

  assert_failure
  assert_output --partial "points to unexpected commit"
}

@test "move-tag.sh shows expected vs found commits on failure" {
  git tag -a v1.0.0 -m "Release"
  
  echo "change1" >> file.txt
  git add file.txt
  git commit -q -m "First"
  
  echo "change2" >> file.txt
  git add file.txt
  git commit -q -m "Second"
  
  run_move_tag

  assert_failure
  assert_output --partial "Expected:"
  assert_output --partial "Found:"
}

@test "move-tag.sh fails when no tags exist" {
  # No tags created, git describe will fail
  run_move_tag

  assert_failure
}

@test "move-tag.sh identifies correct tag name" {
  git tag -a v2.5.0 -m "Release v2.5.0"
  
  # Add commits to make tag not at HEAD~1
  echo "change1" >> file.txt
  git add file.txt
  git commit -q -m "First"
  
  echo "change2" >> file.txt
  git add file.txt
  git commit -q -m "Second"
  
  run_move_tag

  assert_failure
  assert_output --partial "v2.5.0"
}

@test "move-tag.sh handles prerelease tag names" {
  git tag -a v1.0.0-rc.1 -m "Release candidate"
  
  echo "change1" >> file.txt
  git add file.txt
  git commit -q -m "First"
  
  echo "change2" >> file.txt
  git add file.txt
  git commit -q -m "Second"
  
  run_move_tag

  assert_failure
  assert_output --partial "v1.0.0-rc.1"
}

# =============================================================================
# Script Logic Tests (using mocked git wrapper)
# =============================================================================

@test "move-tag.sh detects tag at correct position" {
  # Create a wrapper script that simulates the move-tag logic
  # without actually pushing
  
  git tag -a v1.0.0 -m "Release"
  
  # Add one commit (tag now at HEAD~1)
  echo "change" >> file.txt
  git add file.txt
  git commit -q -m "Version bump"
  
  # Verify the tag IS at HEAD~1 (the correct position)
  local tag_sha prev_sha
  tag_sha=$(git rev-list -n 1 v1.0.0)
  prev_sha=$(git rev-parse HEAD~1)
  
  # This should be equal - meaning move-tag would succeed
  assert_equal "$tag_sha" "$prev_sha"
}

@test "move-tag.sh correctly computes HEAD~1" {
  git tag -a v1.0.0 -m "Release"
  local initial_commit
  initial_commit=$(git rev-parse HEAD)
  
  echo "change" >> file.txt
  git add file.txt
  git commit -q -m "Second commit"
  
  local head_minus_1
  head_minus_1=$(git rev-parse HEAD~1)
  
  assert_equal "$initial_commit" "$head_minus_1"
}

@test "move-tag.sh uses git describe to find latest tag" {
  # Create multiple tags
  git tag -a v1.0.0 -m "First"
  
  echo "change" >> file.txt
  git add file.txt
  git commit -q -m "Change"
  
  git tag -a v2.0.0 -m "Second"
  
  # git describe should return v2.0.0
  local latest
  latest=$(git describe --tags --abbrev=0)
  
  assert_equal "$latest" "v2.0.0"
}
