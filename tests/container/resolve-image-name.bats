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

# Run resolve-image-name with debug output
run_resolve_image_name() {
  run_script "container/resolve-image-name.sh" "$@"
}

# =============================================================================
# GHCR Registry Tests
# =============================================================================

@test "resolve-image-name prefixes ghcr.io for simple name" {
  run_resolve_image_name "ghcr.io" "myapp" "owner/repo" "owner"

  assert_success
  assert_output "name=ghcr.io/myapp"
}

@test "resolve-image-name uses repository as default image name" {
  run_resolve_image_name "ghcr.io" "" "myorg/myrepo" "myorg"

  assert_success
  assert_output "name=ghcr.io/myorg/myrepo"
}

@test "resolve-image-name handles ghcr.io with org prefix" {
  run_resolve_image_name "ghcr.io" "backend" "diggsweden/workflow" "diggsweden"

  assert_success
  assert_output "name=ghcr.io/backend"
}

# =============================================================================
# Docker Hub Registry Tests
# =============================================================================

@test "resolve-image-name prefixes docker.io with owner" {
  run_resolve_image_name "docker.io" "myapp" "owner/repo" "owner"

  assert_success
  assert_output "name=owner/myapp"
}

@test "resolve-image-name uses owner/repo format for docker.io" {
  run_resolve_image_name "docker.io" "nginx-custom" "myuser/myproject" "myuser"

  assert_success
  assert_output "name=myuser/nginx-custom"
}

@test "resolve-image-name handles docker.io empty image name" {
  run_resolve_image_name "docker.io" "" "username/project" "username"

  assert_success
  assert_output "name=username/username/project"
}

# =============================================================================
# Fully Qualified Image Names
# =============================================================================

@test "resolve-image-name handles name with slash" {
  run_resolve_image_name "ghcr.io" "myorg/myapp" "owner/repo" "owner"

  assert_success
  # Script logic: if name contains slash but no dot, it still prefixes registry
  assert_output "name=ghcr.io/myorg/myapp"
}

@test "resolve-image-name preserves fully qualified name with dot" {
  run_resolve_image_name "ghcr.io" "registry.example.com/myapp" "owner/repo" "owner"

  assert_success
  # Name contains dot, should be used as-is
  assert_output "name=registry.example.com/myapp"
}

@test "resolve-image-name preserves full ghcr.io path" {
  run_resolve_image_name "ghcr.io" "ghcr.io/myorg/myimage" "owner/repo" "owner"

  assert_success
  assert_output "name=ghcr.io/myorg/myimage"
}

# =============================================================================
# Other Registry Tests
# =============================================================================

@test "resolve-image-name handles ECR registry" {
  run_resolve_image_name "123456789.dkr.ecr.us-east-1.amazonaws.com" "myapp" "org/repo" "org"

  assert_success
  assert_output "name=123456789.dkr.ecr.us-east-1.amazonaws.com/myapp"
}

@test "resolve-image-name handles quay.io registry" {
  run_resolve_image_name "quay.io" "myimage" "org/repo" "org"

  assert_success
  assert_output "name=quay.io/myimage"
}

@test "resolve-image-name handles custom registry" {
  run_resolve_image_name "registry.internal.example.com" "myservice" "team/project" "team"

  assert_success
  assert_output "name=registry.internal.example.com/myservice"
}

# =============================================================================
# Edge Cases
# =============================================================================

@test "resolve-image-name handles hyphenated image name" {
  run_resolve_image_name "ghcr.io" "my-awesome-app" "owner/repo" "owner"

  assert_success
  assert_output "name=ghcr.io/my-awesome-app"
}

@test "resolve-image-name handles underscored image name" {
  run_resolve_image_name "ghcr.io" "my_app_name" "owner/repo" "owner"

  assert_success
  assert_output "name=ghcr.io/my_app_name"
}

@test "resolve-image-name handles numeric image name" {
  run_resolve_image_name "ghcr.io" "app123" "owner/repo" "owner"

  assert_success
  assert_output "name=ghcr.io/app123"
}

# =============================================================================
# Repository Format Tests
# =============================================================================

@test "resolve-image-name handles org/repo format" {
  run_resolve_image_name "ghcr.io" "" "myorganization/myrepository" "myorganization"

  assert_success
  assert_output "name=ghcr.io/myorganization/myrepository"
}

@test "resolve-image-name handles user/repo format" {
  run_resolve_image_name "docker.io" "" "johndoe/myproject" "johndoe"

  assert_success
  assert_output "name=johndoe/johndoe/myproject"
}

# =============================================================================
# Output Format Tests
# =============================================================================

@test "resolve-image-name outputs in key=value format" {
  run_resolve_image_name "ghcr.io" "test" "owner/repo" "owner"

  assert_success
  assert_output --regexp "^name=.+"
}

@test "resolve-image-name output is single line" {
  run_resolve_image_name "ghcr.io" "test" "owner/repo" "owner"

  assert_success
  local line_count
  line_count=$(echo "$output" | wc -l)
  assert [ "$line_count" -eq 1 ]
}

# =============================================================================
# Integration with GitHub Actions
# =============================================================================

@test "resolve-image-name output is usable in GITHUB_OUTPUT" {
  run_resolve_image_name "ghcr.io" "myapp" "owner/repo" "owner"

  assert_success
  # Output should be in format that can be appended to GITHUB_OUTPUT
  assert_output "name=ghcr.io/myapp"
  
  # Simulate writing to GITHUB_OUTPUT
  echo "$output" >> "$TEST_DIR/github_output"
  run cat "$TEST_DIR/github_output"
  assert_output "name=ghcr.io/myapp"
}
