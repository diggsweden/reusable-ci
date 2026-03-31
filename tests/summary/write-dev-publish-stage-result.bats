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

@test "maven project all success" {
  export PROJECT_TYPE="maven"
  export BUILD_DEV_CONTAINER_RESULT="success"
  export PUBLISH_NPM_DEV_RESULT="skipped"
  export CONTAINER_IMAGE="ghcr.io/org/app"
  export CONTAINER_DIGEST="sha256:abc123"

  run_script "summary/write-dev-publish-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "true"

  run get_github_output "stage-result"
  assert_output "success"
}

@test "all skipped by default when no env vars set" {
  run_script "summary/write-dev-publish-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "false"

  run get_github_output "stage-result"
  assert_output "skipped"
}

@test "container failure propagates to stage result" {
  export PROJECT_TYPE="npm"
  export BUILD_DEV_CONTAINER_RESULT="failure"
  export PUBLISH_NPM_DEV_RESULT="success"
  export PUBLISH_NPM="true"

  run_script "summary/write-dev-publish-stage-result.sh"
  assert_success

  run get_github_output "stage-result"
  assert_output "failure"
}

@test "npm publish cancelled propagates to stage result" {
  export PROJECT_TYPE="npm"
  export BUILD_DEV_CONTAINER_RESULT="success"
  export PUBLISH_NPM_DEV_RESULT="cancelled"
  export PUBLISH_NPM="true"

  run_script "summary/write-dev-publish-stage-result.sh"
  assert_success

  run get_github_output "stage-result"
  assert_output "cancelled"
}

@test "failure takes precedence over cancelled" {
  export PROJECT_TYPE="npm"
  export BUILD_DEV_CONTAINER_RESULT="failure"
  export PUBLISH_NPM_DEV_RESULT="cancelled"
  export PUBLISH_NPM="true"

  run_script "summary/write-dev-publish-stage-result.sh"
  assert_success

  run get_github_output "stage-result"
  assert_output "failure"
}

@test "unknown result values normalize to skipped" {
  export PROJECT_TYPE="gradle"
  export BUILD_DEV_CONTAINER_RESULT="weird_value"

  run_script "summary/write-dev-publish-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "true"

  run get_github_output "stage-result"
  assert_output "success"
}

@test "stage-ran is true for maven npm and gradle project types" {
  for pt in maven npm gradle; do
    export PROJECT_TYPE="$pt"
    : >"$GITHUB_OUTPUT"

    run_script "summary/write-dev-publish-stage-result.sh"
    assert_success

    run get_github_output "stage-ran"
    assert_output "true"
  done
}

@test "stage-ran is false for unknown project type" {
  export PROJECT_TYPE="rust"

  run_script "summary/write-dev-publish-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "false"

  run get_github_output "stage-result"
  assert_output "skipped"
}

@test "npm target is skipped when project type is not npm" {
  export PROJECT_TYPE="maven"
  export BUILD_DEV_CONTAINER_RESULT="success"
  export PUBLISH_NPM_DEV_RESULT="success"

  run_script "summary/write-dev-publish-stage-result.sh"
  assert_success

  run get_github_output "result-json"
  assert_output --partial '"npm":"skipped"'
}

@test "npm target is skipped when publish npm is false" {
  export PROJECT_TYPE="npm"
  export PUBLISH_NPM="false"
  export PUBLISH_NPM_DEV_RESULT="success"

  run_script "summary/write-dev-publish-stage-result.sh"
  assert_success

  run get_github_output "result-json"
  assert_output --partial '"npm":"skipped"'
}

@test "npm target uses result when project type is npm and publish npm is true" {
  export PROJECT_TYPE="npm"
  export PUBLISH_NPM="true"
  export PUBLISH_NPM_DEV_RESULT="success"
  export BUILD_DEV_CONTAINER_RESULT="success"

  run_script "summary/write-dev-publish-stage-result.sh"
  assert_success

  run get_github_output "result-json"
  assert_output --partial '"npm":"success"'
}

@test "result-json contains expected structure" {
  export PROJECT_TYPE="npm"
  export BUILD_DEV_CONTAINER_RESULT="success"
  export PUBLISH_NPM_DEV_RESULT="success"
  export PUBLISH_NPM="true"
  export CONTAINER_IMAGE="ghcr.io/org/app"
  export CONTAINER_DIGEST="sha256:abc123"

  run_script "summary/write-dev-publish-stage-result.sh"
  assert_success

  run get_github_output "result-json"
  assert_output --partial '"stage":"dev-publish"'
  assert_output --partial '"result":"success"'
  assert_output --partial '"ran":true'
  assert_output --partial '"project_type":"npm"'
  assert_output --partial '"container":"success"'
  assert_output --partial '"npm":"success"'
}

@test "artifacts-json contains container and npm details" {
  export PROJECT_TYPE="npm"
  export BUILD_DEV_CONTAINER_RESULT="success"
  export CONTAINER_IMAGE="ghcr.io/org/app"
  export CONTAINER_DIGEST="sha256:abc123"
  export NPM_PACKAGE_NAME="@org/app"
  export NPM_PACKAGE_VERSION="1.0.0-dev.1"

  run_script "summary/write-dev-publish-stage-result.sh"
  assert_success

  run get_github_output "artifacts-json"
  assert_output --partial '"container_image":"ghcr.io/org/app"'
  assert_output --partial '"container_digest":"sha256:abc123"'
  assert_output --partial '"npm_package_name":"@org/app"'
  assert_output --partial '"npm_package_version":"1.0.0-dev.1"'
  assert_output --partial '"npm_publish_status":"published"'
}

@test "artifacts-json contains already-exists status when set" {
  export PROJECT_TYPE="npm"
  export BUILD_DEV_CONTAINER_RESULT="success"
  export NPM_PACKAGE_NAME="@org/app"
  export NPM_PACKAGE_VERSION="1.0.0-dev.1"
  export NPM_PUBLISH_STATUS="already-exists"

  run_script "summary/write-dev-publish-stage-result.sh"
  assert_success

  run get_github_output "artifacts-json"
  assert_output --partial '"npm_publish_status":"already-exists"'
}

@test "artifacts-json defaults publish status to published when not set" {
  export PROJECT_TYPE="npm"
  export BUILD_DEV_CONTAINER_RESULT="success"
  export NPM_PACKAGE_NAME="@org/app"
  export NPM_PACKAGE_VERSION="1.0.0-dev.1"

  run_script "summary/write-dev-publish-stage-result.sh"
  assert_success

  run get_github_output "artifacts-json"
  assert_output --partial '"npm_publish_status":"published"'
}
