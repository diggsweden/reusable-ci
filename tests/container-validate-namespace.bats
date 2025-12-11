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

setup() {
  common_setup
}

teardown() {
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

# Run validate-namespace with standard test arguments
# Usage: run_validate_namespace <image> <repo> <registry> <owner>
run_validate_namespace() {
  run_script "container/validate-namespace.sh" "$@"
}

# =============================================================================
# Input Validation Tests
# =============================================================================

@test "validate-namespace.sh requires all arguments" {
  run_validate_namespace

  assert_failure
  # Usage message goes to stderr
  [[ "$stderr" == *"Usage"* ]]
}

@test "validate-namespace.sh shows usage when missing arguments" {
  run_validate_namespace "ghcr.io/myorg/myrepo"

  assert_failure
  # Usage message goes to stderr
  [[ "$stderr" == *"Usage"* ]]
}

# =============================================================================
# Valid Namespace Tests
# =============================================================================

@test "validate-namespace.sh accepts valid ghcr.io namespace" {
  run_validate_namespace \
    "ghcr.io/myorg/myrepo" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_success
  assert_output --partial "namespace validated"
}

@test "validate-namespace.sh accepts valid ghcr.io namespace with suffix" {
  run_validate_namespace \
    "ghcr.io/myorg/myrepo-api" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_success
  assert_output --partial "namespace validated"
}

@test "validate-namespace.sh handles repository with org prefix" {
  run_validate_namespace \
    "ghcr.io/diggsweden/my-app" \
    "diggsweden/my-app" \
    "ghcr.io" \
    "diggsweden"

  assert_success
  assert_output --partial "namespace validated"
}

@test "validate-namespace.sh rejects multi-level image names not matching repo" {
  # Multi-level paths like /backend are not allowed - must start with exact repo name
  run_validate_namespace \
    "ghcr.io/myorg/myrepo/backend" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_failure
  assert_output --partial "must start with"
}

# =============================================================================
# Invalid Namespace Tests
# =============================================================================

@test "validate-namespace.sh rejects wrong namespace" {
  run_validate_namespace \
    "ghcr.io/otherorg/myrepo" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_failure
  assert_output --partial "Security"
  assert_output --partial "unauthorized namespaces"
}

@test "validate-namespace.sh rejects wrong repository" {
  run_validate_namespace \
    "ghcr.io/myorg/wrongrepo" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_failure
  assert_output --partial "must start with"
}

@test "validate-namespace.sh rejects completely mismatched image" {
  run_validate_namespace \
    "ghcr.io/badorg/badrepo" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_failure
  assert_output --partial "ghcr.io/badorg/badrepo"
}

# =============================================================================
# Non-GHCR Registry Tests
# =============================================================================

@test "validate-namespace.sh skips validation for non-ghcr registries" {
  run_validate_namespace \
    "docker.io/anything/image" \
    "myorg/myrepo" \
    "docker.io" \
    "myorg"

  assert_success
  assert_output --partial "Non-GHCR registry"
  assert_output --partial "skipped"
}

@test "validate-namespace.sh skips validation for ECR registry" {
  run_validate_namespace \
    "123456789.dkr.ecr.us-east-1.amazonaws.com/myapp" \
    "myorg/myrepo" \
    "123456789.dkr.ecr.us-east-1.amazonaws.com" \
    "myorg"

  assert_success
  assert_output --partial "Non-GHCR registry"
}

@test "validate-namespace.sh skips validation for quay.io registry" {
  run_validate_namespace \
    "quay.io/myorg/myapp" \
    "myorg/myrepo" \
    "quay.io" \
    "myorg"

  assert_success
  assert_output --partial "Non-GHCR registry"
}

# =============================================================================
# Error Message Tests
# =============================================================================

@test "validate-namespace.sh shows image name in error" {
  run_validate_namespace \
    "ghcr.io/badorg/badrepo" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_failure
  assert_output --partial "ghcr.io/badorg/badrepo"
}

@test "validate-namespace.sh shows expected prefix in error" {
  run_validate_namespace \
    "ghcr.io/wrong/image" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_failure
  assert_output --partial "ghcr.io/myorg/myrepo"
}

# =============================================================================
# Output Display Tests
# =============================================================================

@test "validate-namespace.sh displays validation info" {
  run_validate_namespace \
    "ghcr.io/myorg/myrepo" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_success
  assert_output --partial "Image: ghcr.io/myorg/myrepo"
  assert_output --partial "Repository: myorg/myrepo"
  assert_output --partial "Registry: ghcr.io"
}

@test "validate-namespace.sh shows enforced namespace in output" {
  run_validate_namespace \
    "ghcr.io/myorg/myrepo" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_success
  assert_output --partial "Enforced namespace: myorg"
}
