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
  common_setup
}

teardown() {
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

run_validate_authorization() {
  run_script "validation/validate-authorization.sh" "$@"
}

# =============================================================================
# Input Validation Tests
# =============================================================================

@test "validate-authorization.sh requires tag-name argument" {
  run_validate_authorization

  assert_failure
  assert_stderr_contains "Usage"
}

@test "validate-authorization.sh requires actor argument" {
  run_validate_authorization "v1.0.0"

  assert_failure
  assert_stderr_contains "Usage"
}

# =============================================================================
# SNAPSHOT Release Tests (Always Allowed)
# =============================================================================

@test "validate-authorization.sh allows any user for SNAPSHOT releases" {
  run_validate_authorization "v1.0.0-SNAPSHOT" "any-user" "authorized-only"

  assert_success
  assert_output --partial "SNAPSHOT release"
  assert_output --partial "authorization check skipped"
}

@test "validate-authorization.sh allows SNAPSHOT with uppercase suffix" {
  run_validate_authorization "v2.0.0-SNAPSHOT" "random-user" "admin1,admin2"

  assert_success
  assert_output --partial "SNAPSHOT"
}

@test "validate-authorization.sh allows SNAPSHOT even with no authorized devs" {
  run_validate_authorization "v1.0.0-SNAPSHOT" "user" ""

  assert_success
}

# =============================================================================
# No Authorization Configured Tests
# =============================================================================

@test "validate-authorization.sh allows all users when no authorized devs configured" {
  run_validate_authorization "v1.0.0" "random-user" ""

  assert_success
  assert_output --partial "warning"
  assert_output --partial "AUTHORIZED_RELEASE_DEVELOPERS secret not configured"
  assert_output --partial "Authorization check passed"
}

@test "validate-authorization.sh warns about open access when no restrictions" {
  run_validate_authorization "v2.5.0" "some-developer" ""

  assert_success
  assert_output --partial "All users with tag push access can create releases"
}

# =============================================================================
# Authorized User Tests
# =============================================================================

@test "validate-authorization.sh allows authorized user" {
  run_validate_authorization "v1.0.0" "admin" "admin,developer"

  assert_success
  assert_output --partial "User 'admin' is authorized"
}

@test "validate-authorization.sh allows user from middle of list" {
  run_validate_authorization "v1.0.0" "developer" "admin,developer,maintainer"

  assert_success
  assert_output --partial "User 'developer' is authorized"
}

@test "validate-authorization.sh allows user at end of list" {
  run_validate_authorization "v1.0.0" "maintainer" "admin,developer,maintainer"

  assert_success
  assert_output --partial "User 'maintainer' is authorized"
}

@test "validate-authorization.sh allows single authorized user" {
  run_validate_authorization "v1.0.0" "only-admin" "only-admin"

  assert_success
  assert_output --partial "User 'only-admin' is authorized"
}

# =============================================================================
# Unauthorized User Tests
# =============================================================================

@test "validate-authorization.sh rejects unauthorized user" {
  run_validate_authorization "v1.0.0" "hacker" "admin,developer"

  assert_failure
  assert_output --partial "User 'hacker' is not authorized"
}

@test "validate-authorization.sh shows error annotation for unauthorized user" {
  run_validate_authorization "v1.0.0" "random-user" "admin"

  assert_failure
  assert_output --partial "::error::"
  assert_output --partial "not authorized"
}

@test "validate-authorization.sh lists authorized users on rejection" {
  run_validate_authorization "v1.0.0" "unauthorized" "alice,bob,charlie"

  assert_failure
  assert_output --partial "alice"
  assert_output --partial "bob"
  assert_output --partial "charlie"
}

@test "validate-authorization.sh suggests SNAPSHOT alternative on rejection" {
  run_validate_authorization "v1.0.0" "random-user" "admin"

  assert_failure
  assert_output --partial "SNAPSHOT release instead"
}

@test "validate-authorization.sh suggests contacting authorized devs" {
  run_validate_authorization "v1.0.0" "random-user" "admin"

  assert_failure
  assert_output --partial "Contact one of the authorized developers"
}

# =============================================================================
# Edge Cases - Username Patterns
# =============================================================================

@test "validate-authorization.sh handles username with hyphen" {
  run_validate_authorization "v1.0.0" "user-name" "user-name,other"

  assert_success
  assert_output --partial "User 'user-name' is authorized"
}

@test "validate-authorization.sh handles username with underscore" {
  run_validate_authorization "v1.0.0" "user_name" "user_name"

  assert_success
}

@test "validate-authorization.sh does not partial match usernames" {
  # User 'admin' should not match 'administrator' or 'superadmin'
  run_validate_authorization "v1.0.0" "admin" "administrator,superadmin"

  assert_failure
  assert_output --partial "User 'admin' is not authorized"
}

@test "validate-authorization.sh handles numeric username" {
  run_validate_authorization "v1.0.0" "user123" "user123,admin"

  assert_success
}

# =============================================================================
# Edge Cases - List Formatting
# =============================================================================

@test "validate-authorization.sh rejects user when list has spaces around commas" {
  # The script uses grep with exact match - spaces break matching
  # This documents current behavior: "admin" won't match " admin" in list
  run_validate_authorization "v1.0.0" "admin" " admin, developer, maintainer"

  # Spaces cause the match to fail (looking for ",admin," but list has ", admin,")
  assert_failure
  assert_output --partial "not authorized"
}

@test "validate-authorization.sh handles trailing comma in list" {
  run_validate_authorization "v1.0.0" "admin" "admin,developer,"

  assert_success
}

# =============================================================================
# Production vs Pre-release Tests
# =============================================================================

@test "validate-authorization.sh enforces auth for production release" {
  run_validate_authorization "v1.0.0" "random" "admin"

  assert_failure
  assert_output --partial "production releases"
}

@test "validate-authorization.sh enforces auth for pre-release (non-SNAPSHOT)" {
  # rc, beta, alpha are NOT SNAPSHOT, so auth is required
  run_validate_authorization "v1.0.0-rc.1" "random" "admin"

  assert_failure
}

@test "validate-authorization.sh enforces auth for beta release" {
  run_validate_authorization "v1.0.0-beta" "random" "admin"

  assert_failure
}
