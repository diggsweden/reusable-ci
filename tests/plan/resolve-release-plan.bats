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

run_resolve_release_plan() {
  run_script "plan/resolve-release-plan.sh" "$@"
}

# =============================================================================
# Helper Functions
# =============================================================================

set_standard_plan_env() {
  export RELEASE_TYPE="stable"
  export RELEASE_PUBLISHER="github-cli"
  export RELEASE_CHECK_AUTHORIZATION="false"
  export RELEASE_DRAFT="false"
  export RELEASE_GENERATE_SBOM="true"
  export RELEASE_SIGN_ARTIFACTS="true"
  export CHANGELOG_CREATOR="git-cliff"
  export CHANGELOG_SKIP_VERSION_BUMP="false"
  export GITHUB_REF_NAME="v1.2.3"
  export NEEDS_SBOM="true"
  export FIRST_REQUIRE_AUTHORIZATION="true"
  export CONTAINERS='[{"name":"app"}]'
}

# =============================================================================
# Stable Release Tests
# =============================================================================

@test "resolve-release-plan writes stable release outputs" {
  set_standard_plan_env

  run_resolve_release_plan

  assert_success
  run get_github_output should-make-latest
  assert_output "true"
  run get_github_output has-containers
  assert_output "true"
  run get_github_output should-generate-sbom
  assert_output "true"
  run get_github_output should-sign-artifacts
  assert_output "true"
  run get_github_output should-create-release
  assert_output "true"
  run get_github_output should-check-authorization
  assert_output "true"
  run get_github_output should-run-version-bump
  assert_output "true"
  run get_github_output should-create-draft-release
  assert_output "false"
}

@test "resolve-release-plan writes prerelease fallback outputs" {
  export RELEASE_TYPE=""
  export RELEASE_PUBLISHER=""
  export RELEASE_CHECK_AUTHORIZATION="false"
  export RELEASE_DRAFT="false"
  export RELEASE_GENERATE_SBOM="false"
  export RELEASE_SIGN_ARTIFACTS="false"
  export CHANGELOG_CREATOR=""
  export CHANGELOG_SKIP_VERSION_BUMP="true"
  export GITHUB_REF_NAME="v1.2.3-beta.1"
  export NEEDS_SBOM="false"
  export FIRST_REQUIRE_AUTHORIZATION="false"
  export CONTAINERS='[]'

  run_resolve_release_plan

  assert_success
  run get_github_output should-make-latest
  assert_output "false"
  run get_github_output has-containers
  assert_output "false"
  run get_github_output should-generate-sbom
  assert_output "false"
  run get_github_output should-sign-artifacts
  assert_output "false"
  run get_github_output should-create-release
  assert_output "false"
  run get_github_output should-check-authorization
  assert_output "false"
  run get_github_output should-run-version-bump
  assert_output "false"
  run get_github_output should-create-draft-release
  assert_output "false"
}

# =============================================================================
# Draft Release Detection Tests
# =============================================================================

@test "resolve-release-plan detects SNAPSHOT as draft release" {
  set_standard_plan_env
  export GITHUB_REF_NAME="v1.0.0-SNAPSHOT"

  run_resolve_release_plan

  assert_success
  run get_github_output should-create-draft-release
  assert_output "true"
}

@test "resolve-release-plan detects lowercase snapshot as draft release" {
  set_standard_plan_env
  export GITHUB_REF_NAME="v1.0.0-snapshot"

  run_resolve_release_plan

  assert_success
  run get_github_output should-create-draft-release
  assert_output "true"
}

@test "resolve-release-plan detects non-semver tag as draft release" {
  set_standard_plan_env
  export GITHUB_REF_NAME="my-custom-tag"

  run_resolve_release_plan

  assert_success
  run get_github_output should-create-draft-release
  assert_output "true"
}

@test "resolve-release-plan does not draft for stable semver tag" {
  set_standard_plan_env
  export RELEASE_DRAFT="false"
  export GITHUB_REF_NAME="v1.0.0"

  run_resolve_release_plan

  assert_success
  run get_github_output should-create-draft-release
  assert_output "false"
}

@test "resolve-release-plan does not draft for rc semver tag" {
  set_standard_plan_env
  export RELEASE_DRAFT="false"
  export GITHUB_REF_NAME="v1.0.0-rc.1"

  run_resolve_release_plan

  assert_success
  run get_github_output should-create-draft-release
  assert_output "false"
}

@test "resolve-release-plan honors explicit draft flag" {
  set_standard_plan_env
  export RELEASE_DRAFT="true"
  export GITHUB_REF_NAME="v1.0.0"

  run_resolve_release_plan

  assert_success
  run get_github_output should-create-draft-release
  assert_output "true"
}
