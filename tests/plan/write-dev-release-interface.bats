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

set_all_dev_vars() {
  export PROJECT_TYPE="npm"
  export BRANCH="feature/my-branch"
  export RELEASE_SHA="abc123def456"
  export RELEASE_ACTOR="test-user"
  export RELEASE_REPOSITORY="org/repo"
  export WORKING_DIRECTORY="."
  export JAVA_VERSION="21"
  export NODE_VERSION="20"
  export CONTAINER_FILE="Containerfile"
  export REGISTRY="ghcr.io"
  export REUSABLE_CI_REF="v2.5.0"
  export NPM_REGISTRY="https://npm.pkg.github.com"
  export PACKAGE_SCOPE="@myorg"
  export NPM_REGISTRY_USERNAME="deploy-bot"
  export PUBLISH_NPM="true"
  export USE_CI_TOKEN="true"
}

@test "write-dev-release-interface succeeds with all vars set" {
  set_all_dev_vars

  run_script "plan/write-dev-release-interface.sh"

  assert_success
}

@test "dev-context-json contains all expected fields" {
  set_all_dev_vars

  run_script "plan/write-dev-release-interface.sh"
  assert_success

  run get_github_output "dev-context-json"
  assert_output --partial '"project_type":"npm"'
  assert_output --partial '"branch":"feature/my-branch"'
  assert_output --partial '"release_sha":"abc123def456"'
  assert_output --partial '"release_actor":"test-user"'
  assert_output --partial '"release_repository":"org/repo"'
  assert_output --partial '"working_directory":"."'
  assert_output --partial '"java_version":"21"'
  assert_output --partial '"node_version":"20"'
  assert_output --partial '"container_file":"Containerfile"'
  assert_output --partial '"registry":"ghcr.io"'
  assert_output --partial '"reusable_ci_ref":"v2.5.0"'
  assert_output --partial '"npm_registry":"https://npm.pkg.github.com"'
  assert_output --partial '"package_scope":"@myorg"'
  assert_output --partial '"npm_registry_username":"deploy-bot"'
}

@test "dev-policy-json has correct booleans when both true" {
  set_all_dev_vars

  run_script "plan/write-dev-release-interface.sh"
  assert_success

  run get_github_output "dev-policy-json"
  assert_output '{"publish_npm":true,"use_ci_token":true}'
}

@test "NPM_REGISTRY_USERNAME defaults to empty when not set" {
  set_all_dev_vars
  unset NPM_REGISTRY_USERNAME

  run_script "plan/write-dev-release-interface.sh"
  assert_success

  run get_github_output "dev-context-json"
  assert_output --partial '"npm_registry_username":""'
}

@test "publish_npm false is correctly converted to JSON boolean" {
  set_all_dev_vars
  export PUBLISH_NPM="false"

  run_script "plan/write-dev-release-interface.sh"
  assert_success

  run get_github_output "dev-policy-json"
  assert_output --partial '"publish_npm":false'
  assert_output --partial '"use_ci_token":true'
}

@test "use_ci_token false is correctly converted to JSON boolean" {
  set_all_dev_vars
  export USE_CI_TOKEN="false"

  run_script "plan/write-dev-release-interface.sh"
  assert_success

  run get_github_output "dev-policy-json"
  assert_output --partial '"publish_npm":true'
  assert_output --partial '"use_ci_token":false'
}

@test "both policy flags false produces all false in policy" {
  set_all_dev_vars
  export PUBLISH_NPM="false"
  export USE_CI_TOKEN="false"

  run_script "plan/write-dev-release-interface.sh"
  assert_success

  run get_github_output "dev-policy-json"
  assert_output '{"publish_npm":false,"use_ci_token":false}'
}
