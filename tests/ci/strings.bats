#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/../test_helper.bash"

setup() {
  common_setup
}

teardown() {
  common_teardown
}

# Run sanitize_path_token in a subshell so we exercise it as the contract
# describes (source the helper, call the function, capture stdout).
sanitize() {
  bash -c 'source "$1"; sanitize_path_token "$2"' _ "$SCRIPTS_DIR/ci/strings.sh" "$1"
}

# =============================================================================
# Core rule
# =============================================================================

@test "sanitize_path_token replaces slash with dash (the bug case)" {
  run --separate-stderr sanitize "feat/x"
  assert_success
  assert_output "feat-x"
}

@test "sanitize_path_token handles slashed tag (release/2026.05)" {
  run --separate-stderr sanitize "release/2026.05"
  assert_success
  assert_output "release-2026.05"
}

@test "sanitize_path_token handles PR ref (NN/merge)" {
  run --separate-stderr sanitize "42/merge"
  assert_success
  assert_output "42-merge"
}

@test "sanitize_path_token replaces spaces with dashes" {
  run --separate-stderr sanitize "my feature"
  assert_success
  assert_output "my-feature"
}

# =============================================================================
# Preservation
# =============================================================================

@test "sanitize_path_token leaves clean semver unchanged" {
  run --separate-stderr sanitize "1.2.3"
  assert_success
  assert_output "1.2.3"
}

@test "sanitize_path_token leaves dev-version unchanged" {
  run --separate-stderr sanitize "0.5.9-dev-feat-x-abc1234"
  assert_success
  assert_output "0.5.9-dev-feat-x-abc1234"
}

@test "sanitize_path_token preserves dots and underscores" {
  run --separate-stderr sanitize "v1.2_RC.1"
  assert_success
  assert_output "v1.2_RC.1"
}

@test "sanitize_path_token preserves case" {
  run --separate-stderr sanitize "Feat/X"
  assert_success
  assert_output "Feat-X"
}

# =============================================================================
# Trim and run behaviour
# =============================================================================

@test "sanitize_path_token strips leading and trailing dashes" {
  run --separate-stderr sanitize "--leading-and-trailing--"
  assert_success
  assert_output "leading-and-trailing"
}

@test "sanitize_path_token preserves internal dash runs (no collapse)" {
  run --separate-stderr sanitize "a//b//c"
  assert_success
  assert_output "a--b--c"
}

@test "sanitize_path_token collapses replaced-then-trimmed leading specials" {
  run --separate-stderr sanitize "///foo"
  assert_success
  assert_output "foo"
}

# =============================================================================
# Contract: idempotence
# =============================================================================

@test "sanitize_path_token is idempotent on dirty input" {
  first=$(sanitize "feat/x")
  second=$(sanitize "$first")
  assert_equal "$first" "$second"
  assert_equal "$first" "feat-x"
}

@test "sanitize_path_token is idempotent on already-clean input" {
  first=$(sanitize "0.5.9-dev-feat-x-abc1234")
  second=$(sanitize "$first")
  assert_equal "$first" "$second"
}

# =============================================================================
# Edge cases
# =============================================================================

@test "sanitize_path_token returns empty for empty input" {
  run --separate-stderr sanitize ""
  assert_success
  assert_output ""
}

@test "sanitize_path_token reduces all-special input to empty" {
  run --separate-stderr sanitize "////"
  assert_success
  assert_output ""
}

@test "sanitize_path_token replaces non-ASCII with dashes" {
  run --separate-stderr sanitize "feature-中文"
  assert_success
  # Each non-ASCII byte becomes a dash; trailing dash run is trimmed.
  assert_output "feature"
}
