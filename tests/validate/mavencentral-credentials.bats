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

@test "validate-mavencentral-credentials succeeds when both secrets are set" {
  export MAVENCENTRAL_USERNAME="user"
  export MAVENCENTRAL_PASSWORD="pass"

  run_script "validate/mavencentral-credentials.sh"

  assert_success
  assert_output --partial "Maven Central credentials configured"
}

@test "validate-mavencentral-credentials fails when username is missing" {
  export MAVENCENTRAL_USERNAME=""
  export MAVENCENTRAL_PASSWORD="pass"

  run_script "validate/mavencentral-credentials.sh"

  assert_failure
  assert_output --partial "Missing MAVENCENTRAL_USERNAME secret"
}

@test "validate-mavencentral-credentials fails when password is missing" {
  export MAVENCENTRAL_USERNAME="user"
  export MAVENCENTRAL_PASSWORD=""

  run_script "validate/mavencentral-credentials.sh"

  assert_failure
  assert_output --partial "Missing MAVENCENTRAL_PASSWORD secret"
}
