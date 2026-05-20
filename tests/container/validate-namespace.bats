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

# Run validate-namespace with standard test arguments
# Usage: run_validate_namespace <image> <repo> <registry> <owner>
run_validate_namespace() {
  run_script "container/validate-namespace.sh" "$@"
}

# =============================================================================
# Input Validation Tests
# =============================================================================

@test "validate-namespace requires all arguments" {
  run_validate_namespace

  assert_failure
  # Usage message goes to stderr
  [[ "$stderr" == *"Usage"* ]]
}

@test "validate-namespace shows usage when missing arguments" {
  run_validate_namespace "ghcr.io/myorg/myrepo"

  assert_failure
  # Usage message goes to stderr
  [[ "$stderr" == *"Usage"* ]]
}

# =============================================================================
# Valid Namespace Tests
# =============================================================================

@test "validate-namespace accepts valid ghcr.io namespace" {
  run_validate_namespace \
    "ghcr.io/myorg/myrepo" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_success
  assert_output --partial "namespace validated"
}

@test "validate-namespace accepts valid ghcr.io namespace with suffix" {
  run_validate_namespace \
    "ghcr.io/myorg/myrepo-api" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_success
  assert_output --partial "namespace validated"
}

@test "validate-namespace handles repository with org prefix" {
  run_validate_namespace \
    "ghcr.io/diggsweden/my-app" \
    "diggsweden/my-app" \
    "ghcr.io" \
    "diggsweden"

  assert_success
  assert_output --partial "namespace validated"
}

@test "validate-namespace accepts multi-container subpath under repo prefix" {
  # Multi-container artifacts.yml produces ghcr.io/<org>/<repo>/<container-name>
  # — a subpath under the same org+repo, which is the security boundary.
  run_validate_namespace \
    "ghcr.io/myorg/myrepo/backend" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_success
  assert_output --partial "namespace validated"
}

@test "validate-namespace accepts deeply nested subpath under repo prefix" {
  run_validate_namespace \
    "ghcr.io/myorg/myrepo/services/api" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_success
  assert_output --partial "namespace validated"
}

@test "validate-namespace rejects sibling-prefix sub-namespace (e.g. myrepo-evil/payload)" {
  # 'myrepo-evil/payload' is in the same org but would be a separate
  # top-level package — not a subpath under 'myrepo'. The combination of
  # a '-suffix' AND a '/subpath' in the same image name has no documented
  # use case and enables typosquat-style sibling packages.
  run_validate_namespace \
    "ghcr.io/myorg/myrepo-evil/payload" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_failure
  assert_output --partial "outside the allowed namespace"
}

@test "validate-namespace accepts simple suffix variants without subpaths" {
  # The suffix form remains valid for sibling packages of the same repo.
  run_validate_namespace \
    "ghcr.io/myorg/myrepo-staging" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_success
  assert_output --partial "namespace validated"
}

@test "validate-namespace accepts suffix with internal dashes and dots" {
  # Common variant naming patterns — '-api-v2', '-1.2.3' — must not be
  # mistaken for namespace escapes by the suffix regex.
  run_validate_namespace \
    "ghcr.io/myorg/myrepo-api-v2-rc1" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_success
  assert_output --partial "namespace validated"
}

@test "validate-namespace accepts subpath with internal dashes" {
  # Multi-container name-with-dashes (e.g. 'my-api-service') is the most
  # common shape; dashes inside the subpath segment must not trip the regex.
  run_validate_namespace \
    "ghcr.io/myorg/myrepo/my-api-service" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_success
  assert_output --partial "namespace validated"
}

@test "validate-namespace rejects bare extension of repo name without separator" {
  # 'myrepofoo' starts with 'myrepo' textually but extends the package name
  # without a '-' or '/' separator — that's a different package entirely.
  run_validate_namespace \
    "ghcr.io/myorg/myrepofoo" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_failure
  assert_output --partial "outside the allowed namespace"
}

# =============================================================================
# Invalid Namespace Tests
# =============================================================================

@test "validate-namespace rejects wrong namespace" {
  run_validate_namespace \
    "ghcr.io/otherorg/myrepo" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_failure
  assert_output --partial "Security"
  assert_output --partial "unauthorized namespaces"
}

@test "validate-namespace rejects wrong repository" {
  run_validate_namespace \
    "ghcr.io/myorg/wrongrepo" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_failure
  assert_output --partial "outside the allowed namespace"
}

@test "validate-namespace rejects completely mismatched image" {
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

@test "validate-namespace skips validation for non-ghcr registries" {
  run_validate_namespace \
    "docker.io/anything/image" \
    "myorg/myrepo" \
    "docker.io" \
    "myorg"

  assert_success
  assert_output --partial "Non-GHCR registry"
  assert_output --partial "skipped"
}

@test "validate-namespace skips validation for ECR registry" {
  run_validate_namespace \
    "123456789.dkr.ecr.us-east-1.amazonaws.com/myapp" \
    "myorg/myrepo" \
    "123456789.dkr.ecr.us-east-1.amazonaws.com" \
    "myorg"

  assert_success
  assert_output --partial "Non-GHCR registry"
}

@test "validate-namespace skips validation for quay.io registry" {
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

@test "validate-namespace shows image name in error" {
  run_validate_namespace \
    "ghcr.io/badorg/badrepo" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_failure
  assert_output --partial "ghcr.io/badorg/badrepo"
}

@test "validate-namespace shows expected prefix in error" {
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

@test "validate-namespace displays validation info" {
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

@test "validate-namespace shows enforced namespace in output" {
  run_validate_namespace \
    "ghcr.io/myorg/myrepo" \
    "myorg/myrepo" \
    "ghcr.io" \
    "myorg"

  assert_success
  assert_output --partial "Enforced namespace: myorg"
}
