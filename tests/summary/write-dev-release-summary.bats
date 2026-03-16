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
  export GITHUB_ACTIONS="true"
  export GITHUB_REPOSITORY="org/repo"
  export GITHUB_RUN_ID="12345"
  export PROJECT_TYPE="npm"
  export RELEASE_SHA="def7890abcdef"
  export RELEASE_REF="feat/dev-branch"
  export RELEASE_ACTOR="dev-user"
  export RELEASE_REPOSITORY="org/repo"
  export PUBLISH_STAGE_RESULT_JSON='{"targets":{"container":"success","npm":"success"}}'
  export DEV_ARTIFACTS_JSON='{"container_image":"ghcr.io/org/app:dev","container_digest":"sha256:abc","npm_package_name":"@org/pkg","npm_package_version":"1.0.0-dev"}'
}

teardown() {
  common_teardown
}

@test "write-dev-release-summary creates step summary" {
  run_script "summary/write-dev-release-summary.sh"

  assert_success
  assert_file_not_empty "$GITHUB_STEP_SUMMARY"
}

@test "write-dev-release-summary contains project type" {
  run_script "summary/write-dev-release-summary.sh"

  assert_success
  assert_summary_contains "npm"
}

@test "write-dev-release-summary contains branch" {
  run_script "summary/write-dev-release-summary.sh"

  assert_success
  assert_summary_contains "feat/dev-branch"
}

@test "write-dev-release-summary contains short SHA from RELEASE_SHA" {
  run_script "summary/write-dev-release-summary.sh"

  assert_success
  assert_summary_contains "def7890"
}

@test "write-dev-release-summary contains actor" {
  run_script "summary/write-dev-release-summary.sh"

  assert_success
  assert_summary_contains "dev-user"
}

@test "write-dev-release-summary shows container image when container status is success" {
  run_script "summary/write-dev-release-summary.sh"

  assert_success
  assert_summary_contains "ghcr.io/org/app:dev"
  assert_summary_contains "docker pull ghcr.io/org/app:dev"
}

@test "write-dev-release-summary shows not published when container fails" {
  export PUBLISH_STAGE_RESULT_JSON='{"targets":{"container":"failure","npm":"success"}}'

  run_script "summary/write-dev-release-summary.sh"

  assert_success
  run get_summary
  assert_output --partial "### Container Image"
  assert_output --partial "Not published"
}

@test "write-dev-release-summary shows NPM package for npm project type with success" {
  run_script "summary/write-dev-release-summary.sh"

  assert_success
  assert_summary_contains "@org/pkg@1.0.0-dev"
  assert_summary_contains "npm install @org/pkg@1.0.0-dev"
}

@test "write-dev-release-summary shows not published for NPM when failed" {
  export PUBLISH_STAGE_RESULT_JSON='{"targets":{"container":"success","npm":"failure"}}'

  run_script "summary/write-dev-release-summary.sh"

  assert_success
  run get_summary
  assert_output --partial "### NPM Package"
  assert_output --partial "Not published"
}

@test "write-dev-release-summary contains resources section with links" {
  run_script "summary/write-dev-release-summary.sh"

  assert_success
  assert_summary_contains "Resources"
  assert_summary_contains "Packages"
  assert_summary_contains "Workflow Run"
}
