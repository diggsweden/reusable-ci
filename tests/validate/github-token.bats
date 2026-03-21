#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-mock/stub.bash"
load "${BATS_TEST_DIRNAME}/../test_helper.bash"

# =============================================================================
# Setup / Teardown
# =============================================================================

setup() {
  common_setup
  setup_github_env
}

teardown() {
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

# Run validate-github-token with debug output
run_validate_github_token() {
  run_script "validate/validate-github-token.sh" "$@"
}

# =============================================================================
# Input Validation Tests
# =============================================================================

@test "validate-github-token fails when no token provided" {
  run_validate_github_token "" "owner/repo"

  assert_failure
  assert_output --partial "::error::No GitHub token provided"
}

@test "validate-github-token suggests fine-grained PAT when no token" {
  run_validate_github_token "" "owner/repo"

  assert_failure
  assert_output --partial "fine-grained PAT"
  assert_output --partial "personal-access-tokens"
}

@test "validate-github-token fails when no repository provided" {
  run_validate_github_token "github_pat_test123"

  assert_failure
  assert_output --partial "::error::No repository provided"
}

# =============================================================================
# Classic PAT Rejection Tests
# =============================================================================

@test "validate-github-token rejects classic PAT (ghp_*)" {
  run_validate_github_token "ghp_abcdefg123456" "owner/repo"

  assert_failure
  assert_output --partial "::error::Classic PAT detected"
}

@test "validate-github-token explains why classic PAT is rejected" {
  run_validate_github_token "ghp_xxxxxxxxxxxx" "owner/repo"

  assert_failure
  assert_output --partial "Classic PATs have broad access"
  assert_output --partial "not recommended"
}

@test "validate-github-token suggests fine-grained PAT alternative" {
  run_validate_github_token "ghp_token12345" "owner/repo"

  assert_failure
  assert_output --partial "fine-grained PAT"
  assert_output --partial "github_pat_"
}

@test "validate-github-token provides setup link for new PAT" {
  run_validate_github_token "ghp_test" "owner/repo"

  assert_failure
  assert_output --partial "github.com/settings/personal-access-tokens"
}

# =============================================================================
# Fine-grained PAT Tests (github_pat_*)
# =============================================================================

@test "validate-github-token accepts fine-grained PAT with valid permissions" {
  stub curl "-s -o /dev/null -w %{http_code} -H * https://api.github.com/repos/owner/repo : echo 200"

  run_validate_github_token "github_pat_xxxxxxxxxxxxxxxxxxxx" "owner/repo"

  assert_success
  assert_output --partial "GitHub token validated"
}

@test "validate-github-token fails when fine-grained PAT lacks permissions" {
  stub curl "-s -o /dev/null -w %{http_code} -H * https://api.github.com/repos/owner/repo : echo 404"

  run_validate_github_token "github_pat_xxxxxxxxxxxxxxxxxxxx" "owner/repo"

  assert_failure
  assert_output --partial "::error::Token is invalid or lacks permissions"
  assert_output --partial "HTTP Response: 404"
}

@test "validate-github-token shows permission hint on failure" {
  stub curl "-s -o /dev/null -w %{http_code} -H * https://api.github.com/repos/owner/repo : echo 403"

  run_validate_github_token "github_pat_xxxxxxxxxxxxxxxxxxxx" "owner/repo"

  assert_failure
  assert_output --partial "contents: write"
}

# =============================================================================
# GitHub App Token Tests (ghs_*)
# =============================================================================

@test "validate-github-token accepts GitHub App token with valid permissions" {
  stub curl "-s -o /dev/null -w %{http_code} -H * https://api.github.com/repos/org/repo : echo 200"

  run_validate_github_token "ghs_xxxxxxxxxxxxxxxxxxxx" "org/repo"

  assert_success
  assert_output --partial "GitHub token validated"
}

@test "validate-github-token fails when GitHub App token lacks permissions" {
  stub curl "-s -o /dev/null -w %{http_code} -H * https://api.github.com/repos/org/repo : echo 401"

  run_validate_github_token "ghs_xxxxxxxxxxxxxxxxxxxx" "org/repo"

  assert_failure
  assert_output --partial "Token is invalid or lacks permissions"
}

# =============================================================================
# Unknown Token Type Tests
# =============================================================================

@test "validate-github-token warns on unknown token type" {
  stub curl "-s -o /dev/null -w %{http_code} -H * https://api.github.com/repos/owner/repo : echo 200"

  run_validate_github_token "some_unknown_token_format" "owner/repo"

  assert_success
  assert_output --partial "::warning::Unknown token type"
}

@test "validate-github-token validates unknown token type if API succeeds" {
  stub curl "-s -o /dev/null -w %{http_code} -H * https://api.github.com/repos/test/test : echo 200"

  run_validate_github_token "custom_token_xyz" "test/test"

  assert_success
  assert_output --partial "validated"
}

# =============================================================================
# HTTP Response Code Tests
# =============================================================================

@test "validate-github-token fails on 401 Unauthorized" {
  stub curl "-s -o /dev/null -w %{http_code} -H * https://api.github.com/repos/owner/repo : echo 401"

  run_validate_github_token "github_pat_xxxx" "owner/repo"

  assert_failure
  assert_output --partial "HTTP Response: 401"
}

@test "validate-github-token fails on 403 Forbidden" {
  stub curl "-s -o /dev/null -w %{http_code} -H * https://api.github.com/repos/owner/repo : echo 403"

  run_validate_github_token "github_pat_xxxx" "owner/repo"

  assert_failure
  assert_output --partial "HTTP Response: 403"
}

@test "validate-github-token fails on 404 Not Found" {
  stub curl "-s -o /dev/null -w %{http_code} -H * https://api.github.com/repos/owner/repo : echo 404"

  run_validate_github_token "github_pat_xxxx" "owner/repo"

  assert_failure
  assert_output --partial "HTTP Response: 404"
}

@test "validate-github-token fails on 500 Server Error" {
  stub curl "-s -o /dev/null -w %{http_code} -H * https://api.github.com/repos/owner/repo : echo 500"

  run_validate_github_token "github_pat_xxxx" "owner/repo"

  assert_failure
}

# =============================================================================
# Repository Format Tests
# =============================================================================

@test "validate-github-token handles org/repo format" {
  stub curl "-s -o /dev/null -w %{http_code} -H * https://api.github.com/repos/myorg/myrepo : echo 200"

  run_validate_github_token "github_pat_xxxx" "myorg/myrepo"

  assert_success
}

@test "validate-github-token handles user/repo format" {
  stub curl "-s -o /dev/null -w %{http_code} -H * https://api.github.com/repos/username/project : echo 200"

  run_validate_github_token "github_pat_xxxx" "username/project"

  assert_success
}

@test "validate-github-token handles repo with hyphens" {
  stub curl "-s -o /dev/null -w %{http_code} -H * https://api.github.com/repos/org/my-awesome-repo : echo 200"

  run_validate_github_token "github_pat_xxxx" "org/my-awesome-repo"

  assert_success
}
