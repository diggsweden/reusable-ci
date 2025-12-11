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
  common_setup_with_github_env
}

teardown() {
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

run_generate_status_report() {
  run_script "lint/generate-status-report.sh" "$@"
}

# =============================================================================
# Basic Output Tests
# =============================================================================

@test "generate-status-report.sh creates step summary" {
  run_generate_status_report "Shellcheck|true|success|false"

  assert_success
  [[ -s "$GITHUB_STEP_SUMMARY" ]]
}

@test "generate-status-report.sh outputs PR check status header" {
  run_generate_status_report "Shellcheck|true|success|false"

  assert_success
  run get_summary
  assert_output --partial "Pull Request Check Status"
}

@test "generate-status-report.sh outputs linter results header" {
  run_generate_status_report "Shellcheck|true|success|false"

  assert_success
  run get_summary
  assert_output --partial "Linter Results"
}

# =============================================================================
# Status Display Tests - Pass
# =============================================================================

@test "generate-status-report.sh shows pass status for successful linter" {
  run_generate_status_report "Shellcheck|true|success|false"

  assert_success
  run get_summary
  assert_output --partial "Shellcheck"
  assert_output --partial "Pass"
}

@test "generate-status-report.sh uses checkmark for passing" {
  run_generate_status_report "Shellcheck|true|success|false"

  assert_success
  run get_summary
  assert_output --partial "✓"
}

# =============================================================================
# Status Display Tests - Fail
# =============================================================================

@test "generate-status-report.sh shows fail status for failed linter" {
  run_generate_status_report "Shellcheck|true|failure|false"

  assert_success
  run get_summary
  assert_output --partial "Shellcheck"
  assert_output --partial "Fail"
}

@test "generate-status-report.sh uses X mark for failing" {
  run_generate_status_report "Shellcheck|true|failure|false"

  assert_success
  run get_summary
  assert_output --partial "✗"
}

# =============================================================================
# Status Display Tests - Disabled
# =============================================================================

@test "generate-status-report.sh shows disabled status" {
  run_generate_status_report "Shellcheck|false|success|false"

  assert_success
  run get_summary
  assert_output --partial "Disabled"
}

@test "generate-status-report.sh uses diamond for disabled" {
  run_generate_status_report "Shellcheck|false|success|false"

  assert_success
  run get_summary
  assert_output --partial "🔸"
}

# =============================================================================
# Status Display Tests - Skipped
# =============================================================================

@test "generate-status-report.sh shows skipped status" {
  run_generate_status_report "Shellcheck|true|skipped|false"

  assert_success
  run get_summary
  assert_output --partial "Skipped"
}

@test "generate-status-report.sh uses dash for skipped" {
  run_generate_status_report "Shellcheck|true|skipped|false"

  assert_success
  run get_summary
  assert_output --partial "−"
}

# =============================================================================
# Multiple Linters Tests
# =============================================================================

@test "generate-status-report.sh handles multiple linters" {
  run_generate_status_report \
    "Shellcheck|true|success|false" \
    "Yamllint|true|success|false" \
    "Markdownlint|true|failure|false"

  assert_success
  run get_summary
  assert_output --partial "Shellcheck"
  assert_output --partial "Yamllint"
  assert_output --partial "Markdownlint"
}

@test "generate-status-report.sh shows mixed statuses correctly" {
  run_generate_status_report \
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

@test "generate-status-report.sh shows all passed message when no failures" {
  run_generate_status_report \
    "Shellcheck|true|success|false" \
    "Yamllint|true|success|false"

  assert_success
  run get_summary
  assert_output --partial "All enabled checks passed"
}

@test "generate-status-report.sh shows some failed message when failures exist" {
  run_generate_status_report \
    "Shellcheck|true|success|false" \
    "Yamllint|true|failure|false"

  assert_success
  run get_summary
  assert_output --partial "Some checks failed"
}

@test "generate-status-report.sh explains status job behavior" {
  run_generate_status_report "Shellcheck|true|failure|false"

  assert_success
  run get_summary
  assert_output --partial "status job always succeeds"
}

# =============================================================================
# Deprecated Linter Tests
# =============================================================================

@test "generate-status-report.sh shows deprecation warning" {
  run_generate_status_report "OldLinter|true|success|true"

  assert_success
  run get_summary
  assert_output --partial "DEPRECATED"
}

@test "generate-status-report.sh shows warning banner for deprecated linters" {
  run_generate_status_report "OldLinter|true|success|true"

  assert_success
  run get_summary
  assert_output --partial "WARNING"
}

@test "generate-status-report.sh mentions 3.0.0 removal" {
  run_generate_status_report "OldLinter|true|success|true"

  assert_success
  run get_summary
  assert_output --partial "3.0.0"
}

@test "generate-status-report.sh suggests migration path" {
  run_generate_status_report "OldLinter|true|success|true"

  assert_success
  run get_summary
  assert_output --partial "justmiselint"
}

@test "generate-status-report.sh marks deprecated linter in table" {
  run_generate_status_report "OldLinter|true|success|true"

  assert_success
  run get_summary
  assert_output --partial "OldLinter"
  assert_output --partial "⚠️"
}

@test "generate-status-report.sh lists all deprecated linters in warning" {
  run_generate_status_report \
    "OldLinter1|true|success|true" \
    "OldLinter2|true|success|true" \
    "NewLinter|true|success|false"

  assert_success
  run get_summary
  assert_output --partial "OldLinter1"
  assert_output --partial "OldLinter2"
}

@test "generate-status-report.sh does not show warning for disabled deprecated linter" {
  run_generate_status_report "OldLinter|false|success|true"

  assert_success
  run get_summary
  refute_output --partial "WARNING"
}

# =============================================================================
# Exit Code Tests
# =============================================================================

@test "generate-status-report.sh always exits 0" {
  run_generate_status_report "Shellcheck|true|failure|false"

  assert_success
}

@test "generate-status-report.sh exits 0 even with all failures" {
  run_generate_status_report \
    "Shellcheck|true|failure|false" \
    "Yamllint|true|failure|false"

  assert_success
}

# =============================================================================
# Table Format Tests
# =============================================================================

@test "generate-status-report.sh creates markdown table" {
  run_generate_status_report "Shellcheck|true|success|false"

  assert_success
  run get_summary
  assert_output --partial "| Linter | Status |"
  assert_output --partial "|--------|--------|"
}

@test "generate-status-report.sh formats each linter as table row" {
  run_generate_status_report "Shellcheck|true|success|false"

  assert_success
  run get_summary
  assert_output --regexp "\| Shellcheck \|.*\|"
}

# =============================================================================
# Edge Cases
# =============================================================================

@test "generate-status-report.sh handles no arguments" {
  run_generate_status_report

  assert_success
  run get_summary
  assert_output --partial "Pull Request Check Status"
}

@test "generate-status-report.sh handles linter names with spaces" {
  run_generate_status_report "Shell Check|true|success|false"

  assert_success
  run get_summary
  assert_output --partial "Shell Check"
}

@test "generate-status-report.sh handles special characters in linter names" {
  run_generate_status_report "check-yaml|true|success|false"

  assert_success
  run get_summary
  assert_output --partial "check-yaml"
}
