#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2155
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

# Run validate-auth with debug output
run_validate_auth() {
  run_script "registry/validate-auth.sh" "$@"
}

# =============================================================================
# Valid Configuration Tests - GitHub Token
# =============================================================================

@test "validate-auth.sh succeeds with GITHUB_TOKEN for ghcr.io" {
  run_validate_auth "true" "ghcr.io" "ghcr.io" "false"

  assert_success
  assert_output --partial "valid"
}

@test "validate-auth.sh succeeds with GITHUB_TOKEN and password for ghcr.io" {
  run_validate_auth "true" "ghcr.io" "ghcr.io" "true"

  assert_success
}

# =============================================================================
# Valid Configuration Tests - Custom Auth
# =============================================================================

@test "validate-auth.sh succeeds with custom auth and password for docker.io" {
  run_validate_auth "false" "docker.io" "ghcr.io" "true"

  assert_success
  assert_output --partial "valid"
}

@test "validate-auth.sh succeeds with custom auth for any registry" {
  run_validate_auth "false" "registry.example.com" "ghcr.io" "true"

  assert_success
}

@test "validate-auth.sh succeeds with custom auth for ECR" {
  run_validate_auth "false" "123456789.dkr.ecr.us-east-1.amazonaws.com" "ghcr.io" "true"

  assert_success
}

# =============================================================================
# Invalid Configuration Tests - Missing Password
# =============================================================================

@test "validate-auth.sh fails when custom auth without password" {
  run_validate_auth "false" "docker.io" "ghcr.io" "false"

  assert_failure
  assert_output --partial "error"
  assert_output --partial "registry-password secret is required"
}

@test "validate-auth.sh fails for any registry without password when not using github token" {
  run_validate_auth "false" "registry.example.com" "ghcr.io" "false"

  assert_failure
  assert_output --partial "registry-password"
}

@test "validate-auth.sh error message mentions use-github-token=false" {
  run_validate_auth "false" "docker.io" "ghcr.io" "false"

  assert_failure
  assert_output --partial "use-github-token=false"
}

# =============================================================================
# Warning Tests - Registry Mismatch
# =============================================================================

@test "validate-auth.sh warns when using GITHUB_TOKEN with docker.io" {
  run_validate_auth "true" "docker.io" "ghcr.io" "false"

  assert_success  # Warns but doesn't fail
  assert_output --partial "warning"
  assert_output --partial "non-ghcr.io"
}

@test "validate-auth.sh warns when using GITHUB_TOKEN with custom registry" {
  run_validate_auth "true" "registry.example.com" "ghcr.io" "false"

  assert_success
  assert_output --partial "warning"
}

@test "validate-auth.sh warns about likely failure with mismatched registry" {
  run_validate_auth "true" "docker.io" "ghcr.io" "false"

  assert_success
  assert_output --partial "will likely fail"
}

@test "validate-auth.sh suggests providing registry-password on mismatch" {
  run_validate_auth "true" "docker.io" "ghcr.io" "false"

  assert_success
  assert_output --partial "use-github-token=false"
  assert_output --partial "registry-password"
}

# =============================================================================
# No Warning Tests - Correct Configuration
# =============================================================================

@test "validate-auth.sh does not warn when registry matches expected" {
  run_validate_auth "true" "ghcr.io" "ghcr.io" "false"

  assert_success
  refute_output --partial "warning"
}

@test "validate-auth.sh does not warn when using custom auth" {
  run_validate_auth "false" "docker.io" "ghcr.io" "true"

  assert_success
  refute_output --partial "warning"
}

# =============================================================================
# Edge Cases - Different Expected Registries
# =============================================================================

@test "validate-auth.sh handles custom expected registry" {
  run_validate_auth "true" "custom.registry.io" "custom.registry.io" "false"

  assert_success
  refute_output --partial "warning"
}

@test "validate-auth.sh warns when GITHUB_TOKEN used with non-expected registry" {
  run_validate_auth "true" "other.registry.io" "custom.registry.io" "false"

  assert_success
  assert_output --partial "warning"
  assert_output --partial "non-custom.registry.io"
}

# =============================================================================
# GitHub Actions Annotation Tests
# =============================================================================

@test "validate-auth.sh uses ::error:: annotation for failures" {
  run_validate_auth "false" "docker.io" "ghcr.io" "false"

  assert_failure
  assert_output --partial "::error::"
}

@test "validate-auth.sh uses ::warning:: annotation for mismatched registry" {
  run_validate_auth "true" "docker.io" "ghcr.io" "false"

  assert_success
  assert_output --partial "::warning::"
}

# =============================================================================
# Success Message Tests
# =============================================================================

@test "validate-auth.sh shows success checkmark" {
  run_validate_auth "true" "ghcr.io" "ghcr.io" "false"

  assert_success
  assert_output --partial "Registry authentication configuration is valid"
}

# =============================================================================
# Boolean Input Tests
# =============================================================================

@test "validate-auth.sh handles 'true' string for use-github-token" {
  run_validate_auth "true" "ghcr.io" "ghcr.io" "false"

  assert_success
}

@test "validate-auth.sh handles 'false' string for use-github-token" {
  run_validate_auth "false" "docker.io" "ghcr.io" "true"

  assert_success
}

@test "validate-auth.sh handles 'true' string for has-password" {
  run_validate_auth "false" "docker.io" "ghcr.io" "true"

  assert_success
}

@test "validate-auth.sh handles 'false' string for has-password" {
  run_validate_auth "true" "ghcr.io" "ghcr.io" "false"

  assert_success
}

# =============================================================================
# Common Registry Tests
# =============================================================================

@test "validate-auth.sh handles AWS ECR registry" {
  run_validate_auth "false" "123456789012.dkr.ecr.eu-west-1.amazonaws.com" "ghcr.io" "true"

  assert_success
}

@test "validate-auth.sh handles Azure ACR registry" {
  run_validate_auth "false" "myregistry.azurecr.io" "ghcr.io" "true"

  assert_success
}

@test "validate-auth.sh handles Google GCR registry" {
  run_validate_auth "false" "gcr.io" "ghcr.io" "true"

  assert_success
}

@test "validate-auth.sh handles Quay.io registry" {
  run_validate_auth "false" "quay.io" "ghcr.io" "true"

  assert_success
}
