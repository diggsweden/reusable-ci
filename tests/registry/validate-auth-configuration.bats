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

@test "validate-auth env mode detects password presence" {
  export USE_CI_TOKEN="false"
  export TARGET_REGISTRY="registry.example.com"
  export CI_REGISTRY="ghcr.io"
  export REGISTRY_PASSWORD="secret"

  run_script "registry/validate-auth.sh"

  assert_success
  assert_output --partial "Registry authentication configuration is valid"
}

@test "validate-auth env mode detects missing password" {
  export USE_CI_TOKEN="true"
  export TARGET_REGISTRY="ghcr.io"
  export CI_REGISTRY="ghcr.io"
  export REGISTRY_PASSWORD=""

  run_script "registry/validate-auth.sh"

  assert_success
  assert_output --partial "Registry authentication configuration is valid"
}

@test "validate-auth env mode fails when no password and use-ci-token=false" {
  export USE_CI_TOKEN="false"
  export TARGET_REGISTRY="registry.example.com"
  export CI_REGISTRY="ghcr.io"
  export REGISTRY_PASSWORD=""

  run_script "registry/validate-auth.sh"

  assert_failure
  assert_output --partial "registry-password secret is required"
}

@test "validate-auth env mode defaults CI_REGISTRY to ghcr.io" {
  export USE_CI_TOKEN="true"
  export TARGET_REGISTRY="docker.io"
  unset CI_REGISTRY

  run_script "registry/validate-auth.sh"

  assert_success
  assert_output --partial "non-ghcr.io"
}
