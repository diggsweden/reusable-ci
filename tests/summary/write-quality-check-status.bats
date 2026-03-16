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
  common_setup_with_github_env
}

teardown() {
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

run_write_quality_check_status() {
  run_script "summary/write-quality-check-status.sh" "$@"
}

# =============================================================================
# Basic Output Tests
# =============================================================================

@test "write-quality-check-status creates step summary" {
  run_write_quality_check_status "Shellcheck|true|success|false"

  assert_success
  [[ -s "$GITHUB_STEP_SUMMARY" ]]
}

@test "write-quality-check-status outputs PR check status header" {
  run_write_quality_check_status "Shellcheck|true|success|false"

  assert_success
  run get_summary
  assert_output --partial "Pull Request Check Status"
}

@test "write-quality-check-status outputs quality check results header" {
  run_write_quality_check_status "Shellcheck|true|success|false"

  assert_success
  run get_summary
  assert_output --partial "Quality Check Results"
}

# =============================================================================
# Status Display Tests - Pass
# =============================================================================

@test "write-quality-check-status shows pass status for successful linter" {
  run_write_quality_check_status "Shellcheck|true|success|false"

  assert_success
  run get_summary
  assert_output --partial "Shellcheck"
  assert_output --partial "Pass"
}

@test "write-quality-check-status uses checkmark for passing" {
  run_write_quality_check_status "Shellcheck|true|success|false"

  assert_success
  run get_summary
  assert_output --partial "✓"
}

# =============================================================================
# Status Display Tests - Fail
# =============================================================================

@test "write-quality-check-status shows fail status for failed linter" {
  run_write_quality_check_status "Shellcheck|true|failure|false"

  assert_success
  run get_summary
  assert_output --partial "Shellcheck"
  assert_output --partial "Fail"
}

@test "write-quality-check-status uses X mark for failing" {
  run_write_quality_check_status "Shellcheck|true|failure|false"

  assert_success
  run get_summary
  assert_output --partial "✗"
}

# =============================================================================
# Status Display Tests - Disabled
# =============================================================================

@test "write-quality-check-status shows disabled status" {
  run_write_quality_check_status "Shellcheck|false|success|false"

  assert_success
  run get_summary
  assert_output --partial "Disabled"
}

@test "write-quality-check-status uses diamond for disabled" {
  run_write_quality_check_status "Shellcheck|false|success|false"

  assert_success
  run get_summary
  assert_output --partial "🔸"
}

# =============================================================================
# Status Display Tests - Skipped
# =============================================================================

@test "write-quality-check-status shows skipped status" {
  run_write_quality_check_status "Shellcheck|true|skipped|false"

  assert_success
  run get_summary
  assert_output --partial "Skipped"
}

@test "write-quality-check-status uses dash for skipped" {
  run_write_quality_check_status "Shellcheck|true|skipped|false"

  assert_success
  run get_summary
  assert_output --partial "−"
}

# =============================================================================
# Multiple Linters Tests
# =============================================================================

@test "write-quality-check-status handles multiple linters" {
  run_write_quality_check_status \
    "Shellcheck|true|success|false" \
    "Yamllint|true|success|false" \
    "Markdownlint|true|failure|false"

  assert_success
  run get_summary
  assert_output --partial "Shellcheck"
  assert_output --partial "Yamllint"
  assert_output --partial "Markdownlint"
}

@test "write-quality-check-status shows mixed statuses correctly" {
  run_write_quality_check_status \
    "Shellcheck|true|success|false" \
    "Yamllint|false|success|false" \
    "Markdownlint|true|failure|false"

  assert_success
  run get_summary
  # Check each linter has correct status
  assert_output --regexp "Shellcheck.*Pass"
  assert_output --regexp "Yamllint.*Disabled"
  assert_output --regexp "Markdownlint.*Fail"
}

# =============================================================================
# Summary Message Tests
# =============================================================================

@test "write-quality-check-status shows all passed message when no failures" {
  run_write_quality_check_status \
    "Shellcheck|true|success|false" \
    "Yamllint|true|success|false"

  assert_success
  run get_summary
  assert_output --partial "All enabled checks passed"
}

@test "write-quality-check-status shows some failed message when failures exist" {
  run_write_quality_check_status \
    "Shellcheck|true|success|false" \
    "Yamllint|true|failure|false"

  assert_success
  run get_summary
  assert_output --partial "Some checks failed"
}

@test "write-quality-check-status explains status job behavior" {
  run_write_quality_check_status "Shellcheck|true|failure|false"

  assert_success
  run get_summary
  assert_output --partial "status job always succeeds"
}

# =============================================================================
# Deprecated Linter Tests
# =============================================================================

@test "write-quality-check-status shows deprecation warning" {
  run_write_quality_check_status "OldLinter|true|success|true"

  assert_success
  run get_summary
  assert_output --partial "DEPRECATED"
}

@test "write-quality-check-status shows warning banner for deprecated linters" {
  run_write_quality_check_status "OldLinter|true|success|true"

  assert_success
  run get_summary
  assert_output --partial "WARNING"
}

@test "write-quality-check-status mentions 3.0.0 removal" {
  run_write_quality_check_status "OldLinter|true|success|true"

  assert_success
  run get_summary
  assert_output --partial "3.0.0"
}

@test "write-quality-check-status suggests migration path" {
  run_write_quality_check_status "OldLinter|true|success|true"

  assert_success
  run get_summary
  assert_output --partial "devbasecheck"
}

@test "write-quality-check-status marks deprecated linter in table" {
  run_write_quality_check_status "OldLinter|true|success|true"

  assert_success
  run get_summary
  assert_output --partial "OldLinter"
  assert_output --partial "⚠️"
}

@test "write-quality-check-status lists all deprecated linters in warning" {
  run_write_quality_check_status \
    "OldLinter1|true|success|true" \
    "OldLinter2|true|success|true" \
    "NewLinter|true|success|false"

  assert_success
  run get_summary
  assert_output --partial "OldLinter1"
  assert_output --partial "OldLinter2"
}

@test "write-quality-check-status does not show warning for disabled deprecated linter" {
  run_write_quality_check_status "OldLinter|false|success|true"

  assert_success
  run get_summary
  refute_output --partial "WARNING"
}

# =============================================================================
# Exit Code Tests
# =============================================================================

@test "write-quality-check-status always exits 0" {
  run_write_quality_check_status "Shellcheck|true|failure|false"

  assert_success
}

@test "write-quality-check-status exits 0 even with all failures" {
  run_write_quality_check_status \
    "Shellcheck|true|failure|false" \
    "Yamllint|true|failure|false"

  assert_success
}

# =============================================================================
# Table Format Tests
# =============================================================================

@test "write-quality-check-status creates markdown table" {
  run_write_quality_check_status "Shellcheck|true|success|false"

  assert_success
  run get_summary
  assert_output --partial "| Check | Status |"
  assert_output --partial "|-------|--------|"
}

@test "write-quality-check-status formats each linter as table row" {
  run_write_quality_check_status "Shellcheck|true|success|false"

  assert_success
  run get_summary
  assert_output --regexp "\| Shellcheck \|.*\|"
}

# =============================================================================
# Edge Cases
# =============================================================================

@test "write-quality-check-status handles no arguments" {
  run_write_quality_check_status

  assert_success
  run get_summary
  assert_output --partial "Pull Request Check Status"
}

@test "write-quality-check-status handles linter names with spaces" {
  run_write_quality_check_status "Shell Check|true|success|false"

  assert_success
  run get_summary
  assert_output --partial "Shell Check"
}

@test "write-quality-check-status handles special characters in linter names" {
  run_write_quality_check_status "check-yaml|true|success|false"

  assert_success
  run get_summary
  assert_output --partial "check-yaml"
}
