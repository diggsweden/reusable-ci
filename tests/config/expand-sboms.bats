#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
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
}

teardown() {
  common_teardown
}

# =============================================================================
# Helpers
# =============================================================================

run_expand_sboms() {
  run_script "config/expand-sboms.sh" "$@"
}

# =============================================================================
# Shortcuts
# =============================================================================

@test "expand-sboms 'all' expands to three layers" {
  run_expand_sboms "all"

  assert_success
  assert_output '["build","analyzed-artifact","analyzed-container"]'
}

@test "expand-sboms 'none' expands to empty array" {
  run_expand_sboms "none"

  assert_success
  assert_output "[]"
}

# =============================================================================
# Single-layer values
# =============================================================================

@test "expand-sboms 'build' expands to single-element array" {
  run_expand_sboms "build"

  assert_success
  assert_output '["build"]'
}

@test "expand-sboms 'analyzed-artifact' expands to single-element array" {
  run_expand_sboms "analyzed-artifact"

  assert_success
  assert_output '["analyzed-artifact"]'
}

@test "expand-sboms 'analyzed-container' expands to single-element array" {
  run_expand_sboms "analyzed-container"

  assert_success
  assert_output '["analyzed-container"]'
}

# =============================================================================
# Comma-lists
# =============================================================================

@test "expand-sboms comma-list of two layers preserves order" {
  run_expand_sboms "build,analyzed-artifact"

  assert_success
  assert_output '["build","analyzed-artifact"]'
}

@test "expand-sboms comma-list of three layers equals 'all' payload" {
  run_expand_sboms "build,analyzed-artifact,analyzed-container"

  assert_success
  assert_output '["build","analyzed-artifact","analyzed-container"]'
}

@test "expand-sboms tolerates whitespace around commas" {
  run_expand_sboms " build , analyzed-container "

  assert_success
  assert_output '["build","analyzed-container"]'
}

@test "expand-sboms dedupes repeated layer tokens" {
  run_expand_sboms "build,analyzed-container,build"

  assert_success
  assert_output '["build","analyzed-container"]'
}

# =============================================================================
# Validation failures — shortcuts not composable
# =============================================================================

@test "expand-sboms rejects 'all' combined with a layer" {
  run_expand_sboms "all,build"

  assert_failure
  assert_stderr_contains "'all' is a shortcut"
}

@test "expand-sboms rejects 'none' combined with a layer" {
  run_expand_sboms "none,build"

  assert_failure
  assert_stderr_contains "'none' is a shortcut"
}

# =============================================================================
# Validation failures — unknown / empty tokens
# =============================================================================

@test "expand-sboms rejects unknown token" {
  run_expand_sboms "source"

  assert_failure
  assert_stderr_contains "unknown token 'source'"
}

@test "expand-sboms rejects unknown token in comma-list" {
  run_expand_sboms "build,foobar"

  assert_failure
  assert_stderr_contains "unknown token 'foobar'"
}

@test "expand-sboms rejects empty input" {
  run_expand_sboms ""

  assert_failure
  assert_stderr_contains "value required"
}

@test "expand-sboms rejects whitespace-only input" {
  run_expand_sboms "   "

  assert_failure
  assert_stderr_contains "value required"
}

@test "expand-sboms rejects trailing comma" {
  run_expand_sboms "build,"

  assert_failure
  assert_stderr_contains "empty token"
}

@test "expand-sboms rejects leading comma" {
  run_expand_sboms ",build"

  assert_failure
  assert_stderr_contains "empty token"
}

@test "expand-sboms rejects consecutive commas" {
  run_expand_sboms "build,,analyzed-artifact"

  assert_failure
  assert_stderr_contains "empty token"
}

# =============================================================================
# --format comma
# =============================================================================

@test "expand-sboms --format comma 'all' emits comma-list" {
  run_expand_sboms --format comma all

  assert_success
  assert_output "build,analyzed-artifact,analyzed-container"
}

@test "expand-sboms --format comma 'none' emits empty string" {
  run_expand_sboms --format comma none

  assert_success
  assert_output ""
}

@test "expand-sboms --format comma preserves order for comma-list input" {
  run_expand_sboms --format comma "analyzed-container,build"

  assert_success
  assert_output "analyzed-container,build"
}

# =============================================================================
# --exclude
# =============================================================================

@test "expand-sboms --exclude drops one layer from 'all' expansion" {
  run_expand_sboms --exclude analyzed-container all

  assert_success
  assert_output '["build","analyzed-artifact"]'
}

@test "expand-sboms --exclude combines with --format comma" {
  run_expand_sboms --format comma --exclude analyzed-container all

  assert_success
  assert_output "build,analyzed-artifact"
}

@test "expand-sboms --exclude of absent layer is a no-op" {
  run_expand_sboms --exclude analyzed-container build

  assert_success
  assert_output '["build"]'
}

@test "expand-sboms --exclude applied multiple times stacks" {
  run_expand_sboms --exclude analyzed-artifact --exclude analyzed-container all

  assert_success
  assert_output '["build"]'
}

@test "expand-sboms --exclude that eliminates all layers emits empty" {
  run_expand_sboms --format comma --exclude build --exclude analyzed-artifact --exclude analyzed-container all

  assert_success
  assert_output ""
}

# =============================================================================
# CLI validation
# =============================================================================

@test "expand-sboms rejects unknown flag" {
  run_expand_sboms --bogus all

  assert_failure
  assert_stderr_contains "unknown flag"
}

@test "expand-sboms rejects --format with invalid value" {
  run_expand_sboms --format xml all

  assert_failure
  assert_stderr_contains "--format must be json or comma"
}

@test "expand-sboms rejects --exclude without argument" {
  run_expand_sboms --exclude

  assert_failure
  assert_stderr_contains "--exclude requires an argument"
}

@test "expand-sboms requires an argument" {
  run_expand_sboms

  assert_failure
  assert_stderr_contains "missing argument"
}
