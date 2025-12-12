#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

# Shell script linting tests to catch common errors

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/libs/bats-assert/load.bash"

# =============================================================================
# Printf Safety Tests
# =============================================================================

@test "no printf statements start with dash (causes option parsing error)" {
  # printf "- foo" will fail because - is interpreted as an option
  # Use printf -- "- foo" or printf "%s" "- foo" instead

  run grep -rn 'printf "- ' "${BATS_TEST_DIRNAME}/../scripts/" --include="*.sh"

  if [[ -n "$output" ]]; then
    printf "Found printf statements starting with dash:\n%s\n" "$output"
    printf "\nFix by removing the leading dash or using: printf -- \"- ...\"\n"
  fi

  assert_output ""
}

@test "no printf statements start with percent-dash (causes option parsing error)" {
  # printf "%-" patterns are valid format specifiers, skip those
  # But printf "-%" would be invalid

  run bash -c "grep -rn 'printf \"-[^-]' \"${BATS_TEST_DIRNAME}/../scripts/\" --include=\"*.sh\" | grep -v 'printf -- ' || true"

  if [[ -n "$output" ]]; then
    printf "Found potentially unsafe printf statements:\n%s\n" "$output"
  fi

  assert_output ""
}
