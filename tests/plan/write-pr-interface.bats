#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/../test_helper.bash"

setup() {
  common_setup_with_github_env
}

teardown() {
  common_teardown
}

set_all_linters_enabled() {
  export PROJECT_TYPE="maven"
  export REUSABLE_CI_REF="refs/heads/main"
  export BASE_BRANCH="main"
  export LINTER_COMMITLINT="true"
  export LINTER_LICENSLINT="true"
  export LINTER_DEPENDENCYREVIEW="true"
  export LINTER_MEGALINT="true"
  export LINTER_PUBLICCODELINT="true"
  export LINTER_DEVBASECHECK="true"
  export LINTER_SWIFTFORMAT="true"
  export LINTER_SWIFTLINT="true"
  export SAST_OPENGREP="true"
  export SAST_OPENGREP_RULES="p/default"
  export SAST_OPENGREP_FAIL_ON_SEVERITY="high"
}

set_all_linters_disabled() {
  export PROJECT_TYPE="maven"
  export REUSABLE_CI_REF="refs/heads/main"
  export BASE_BRANCH="main"
  export LINTER_COMMITLINT="false"
  export LINTER_LICENSLINT="false"
  export LINTER_DEPENDENCYREVIEW="false"
  export LINTER_MEGALINT="false"
  export LINTER_PUBLICCODELINT="false"
  export LINTER_DEVBASECHECK="false"
  export LINTER_SWIFTFORMAT="false"
  export LINTER_SWIFTLINT="false"
  export SAST_OPENGREP="false"
  export SAST_OPENGREP_RULES="p/default"
  export SAST_OPENGREP_FAIL_ON_SEVERITY="high"
}

@test "write-pr-interface succeeds with all linters enabled" {
  set_all_linters_enabled

  run_script "plan/write-pr-interface.sh"

  assert_success
}

@test "pr-context-json contains project_type, base_branch, and reusable_ci_ref" {
  set_all_linters_enabled
  export PROJECT_TYPE="npm"
  export BASE_BRANCH="develop"
  export REUSABLE_CI_REF="v2.5.0"
  export SAST_OPENGREP_RULES="rules/opengrep.yml"
  export SAST_OPENGREP_FAIL_ON_SEVERITY="medium"

  run_script "plan/write-pr-interface.sh"
  assert_success

  run get_github_output "pr-context-json"
  assert_output --partial '"project_type":"npm"'
  assert_output --partial '"base_branch":"develop"'
  assert_output --partial '"reusable_ci_ref":"v2.5.0"'
  assert_output --partial '"sast_opengrep_rules":"rules/opengrep.yml"'
  assert_output --partial '"sast_opengrep_fail_on_severity":"medium"'
}

@test "pr-policy-json has correct boolean values when all linters enabled" {
  set_all_linters_enabled

  run_script "plan/write-pr-interface.sh"
  assert_success

  run get_github_output "pr-policy-json"
  assert_output --partial '"commitlint":true'
  assert_output --partial '"licenselint":true'
  assert_output --partial '"dependencyreview":true'
  assert_output --partial '"sastopengrep":true'
  assert_output --partial '"megalint":true'
  assert_output --partial '"publiccodelint":true'
  assert_output --partial '"devbasecheck":true'
  assert_output --partial '"swiftformat":true'
  assert_output --partial '"swiftlint":true'
}

@test "swift is true when swiftformat is true" {
  set_all_linters_disabled
  export LINTER_SWIFTFORMAT="true"
  export LINTER_SWIFTLINT="false"

  run_script "plan/write-pr-interface.sh"
  assert_success

  run get_github_output "pr-policy-json"
  assert_output --partial '"swift":true'
}

@test "swift is true when swiftlint is true" {
  set_all_linters_disabled
  export LINTER_SWIFTFORMAT="false"
  export LINTER_SWIFTLINT="true"

  run_script "plan/write-pr-interface.sh"
  assert_success

  run get_github_output "pr-policy-json"
  assert_output --partial '"swift":true'
}

@test "swift is false when both swiftformat and swiftlint are false" {
  set_all_linters_disabled

  run_script "plan/write-pr-interface.sh"
  assert_success

  run get_github_output "pr-policy-json"
  assert_output --partial '"swift":false'
}

@test "all linters disabled produces all false in policy" {
  set_all_linters_disabled

  run_script "plan/write-pr-interface.sh"
  assert_success

  run get_github_output "pr-policy-json"
  assert_output '{"commitlint":false,"licenselint":false,"dependencyreview":false,"sastopengrep":false,"megalint":false,"publiccodelint":false,"devbasecheck":false,"swiftformat":false,"swiftlint":false,"swift":false}'
}
