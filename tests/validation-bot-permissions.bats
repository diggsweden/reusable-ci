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

# Run validate-bot-permissions with debug output
run_validate_bot() {
  run_script "validation/validate-bot-permissions.sh" "$@"
}

# Create gh mock for successful authentication
create_gh_auth_success_mock() {
  create_mock_binary "gh" '
case "$1" in
  api)
    case "$2" in
      user)
        printf "{\"login\": \"release-bot\"}"
        ;;
      repos/*)
        printf "{\"name\": \"repo\"}"
        ;;
    esac
    ;;
esac
'
  use_mock_path
}

# Create gh mock for invalid/expired token
create_gh_auth_invalid_mock() {
  create_mock_binary "gh" '
case "$1" in
  api)
    case "$2" in
      user)
        exit 1
        ;;
    esac
    ;;
esac
'
  use_mock_path
}

# Create gh mock for valid token but no repo access
create_gh_no_repo_access_mock() {
  create_mock_binary "gh" '
case "$1" in
  api)
    case "$2" in
      user)
        printf "{\"login\": \"release-bot\"}"
        ;;
      repos/*)
        exit 1
        ;;
    esac
    ;;
esac
'
  use_mock_path
}

# Create gh mock for limited permissions (no branch access)
create_gh_limited_permissions_mock() {
  create_mock_binary "gh" '
case "$1" in
  api)
    case "$2" in
      user)
        printf "{\"login\": \"release-bot\"}"
        ;;
      repos/owner/repo)
        printf "{\"name\": \"repo\"}"
        ;;
      repos/owner/repo/branches)
        exit 1
        ;;
    esac
    ;;
esac
'
  use_mock_path
}

# =============================================================================
# Input Validation Tests
# =============================================================================

@test "validate-bot-permissions.sh requires repository argument" {
  create_gh_auth_success_mock

  run_validate_bot

  assert_failure
  assert_stderr_contains "Usage"
}

# =============================================================================
# Token Validation Tests
# =============================================================================

@test "validate-bot-permissions.sh succeeds with valid token and full access" {
  create_gh_auth_success_mock

  run_validate_bot "owner/repo"

  assert_success
  assert_output --partial "Bot token is valid"
}

@test "validate-bot-permissions.sh fails with invalid token" {
  create_gh_auth_invalid_mock

  run_validate_bot "owner/repo"

  assert_failure
  assert_output --partial "error"
  assert_output --partial "invalid or expired"
}

@test "validate-bot-permissions.sh fails when token cannot access repository" {
  create_gh_no_repo_access_mock

  run_validate_bot "owner/repo"

  assert_failure
  assert_output --partial "error"
  assert_output --partial "cannot access"
}

# =============================================================================
# Permission Warning Tests
# =============================================================================

@test "validate-bot-permissions.sh warns about limited permissions" {
  create_gh_limited_permissions_mock

  run_validate_bot "owner/repo"

  # Script still succeeds but with warning
  assert_success
  assert_output --partial "warning"
  assert_output --partial "limited permissions"
}

@test "validate-bot-permissions.sh suggests required permissions on warning" {
  create_gh_limited_permissions_mock

  run_validate_bot "owner/repo"

  assert_success
  assert_output --partial "Push commits"
  assert_output --partial "Create and move tags"
  assert_output --partial "Bypass branch protection"
}

# =============================================================================
# Success Message Tests
# =============================================================================

@test "validate-bot-permissions.sh shows success checkmark" {
  create_gh_auth_success_mock

  run_validate_bot "owner/repo"

  assert_success
  assert_output --partial "has repository access"
}

@test "validate-bot-permissions.sh displays validating message" {
  create_gh_auth_success_mock

  run_validate_bot "owner/repo"

  assert_success
  assert_output --partial "Validating bot token permissions"
}

# =============================================================================
# Repository Format Tests
# =============================================================================

@test "validate-bot-permissions.sh handles owner/repo format" {
  create_gh_auth_success_mock

  run_validate_bot "myorg/myrepo"

  assert_success
}

@test "validate-bot-permissions.sh handles hyphenated repository names" {
  create_gh_auth_success_mock

  run_validate_bot "my-org/my-repo-name"

  assert_success
}

@test "validate-bot-permissions.sh handles underscored repository names" {
  create_gh_auth_success_mock

  run_validate_bot "my_org/my_repo_name"

  assert_success
}

# =============================================================================
# Error Message Tests
# =============================================================================

@test "validate-bot-permissions.sh uses GitHub Actions error annotation for invalid token" {
  create_gh_auth_invalid_mock

  run_validate_bot "owner/repo"

  assert_failure
  assert_output --partial "::error::"
}

@test "validate-bot-permissions.sh uses GitHub Actions error annotation for no access" {
  create_gh_no_repo_access_mock

  run_validate_bot "owner/repo"

  assert_failure
  assert_output --partial "::error::"
}

@test "validate-bot-permissions.sh uses GitHub Actions warning annotation for limited access" {
  create_gh_limited_permissions_mock

  run_validate_bot "owner/repo"

  assert_success
  assert_output --partial "::warning::"
}

# =============================================================================
# Edge Cases
# =============================================================================

@test "validate-bot-permissions.sh handles gh api timeout gracefully" {
  create_mock_binary "gh" '
case "$1" in
  api)
    sleep 0.1
    exit 1
    ;;
esac
'
  use_mock_path

  run_validate_bot "owner/repo"

  assert_failure
}

@test "validate-bot-permissions.sh handles empty repository name" {
  create_gh_auth_success_mock

  run_validate_bot ""

  assert_failure
}
