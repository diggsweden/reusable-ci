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

@test "all publish targets succeed with artifacts present" {
  export PUBLISH_MAVEN_REGISTRY_RESULT="success"
  export PUBLISH_MAVEN_CENTRAL_RESULT="success"
  export PUBLISH_APPLE_APPSTORE_RESULT="success"
  export PUBLISH_GOOGLE_PLAY_RESULT="success"
  export BUILD_CONTAINERS_RESULT="success"
  export GITHUBPACKAGES_ARTIFACTS='[{"name":"app.jar"}]'
  export MAVENCENTRAL_ARTIFACTS='[{"name":"app.jar"}]'
  export XCODEIOS_ARTIFACTS='[{"name":"app.ipa"}]'
  export GOOGLEPLAY_ARTIFACTS='[{"name":"app.aab"}]'
  export CONTAINERS='[{"name":"app"}]'

  run_script "summary/write-publish-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "true"

  run get_github_output "stage-result"
  assert_output "success"
}

@test "all skipped by default when no env vars set" {
  run_script "summary/write-publish-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "false"

  run get_github_output "stage-result"
  assert_output "skipped"
}

@test "single failure propagates to stage result" {
  export PUBLISH_MAVEN_REGISTRY_RESULT="success"
  export PUBLISH_APPLE_APPSTORE_RESULT="failure"
  export GITHUBPACKAGES_ARTIFACTS='[{"name":"app.jar"}]'
  export XCODEIOS_ARTIFACTS='[{"name":"app.ipa"}]'

  run_script "summary/write-publish-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "true"

  run get_github_output "stage-result"
  assert_output "failure"
}

@test "single cancelled propagates to stage result" {
  export BUILD_CONTAINERS_RESULT="cancelled"
  export CONTAINERS='[{"name":"app"}]'

  run_script "summary/write-publish-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "true"

  run get_github_output "stage-result"
  assert_output "cancelled"
}

@test "failure takes precedence over cancelled" {
  export PUBLISH_MAVEN_REGISTRY_RESULT="failure"
  export PUBLISH_GOOGLE_PLAY_RESULT="cancelled"
  export GITHUBPACKAGES_ARTIFACTS='[{"name":"app.jar"}]'
  export GOOGLEPLAY_ARTIFACTS='[{"name":"app.aab"}]'

  run_script "summary/write-publish-stage-result.sh"
  assert_success

  run get_github_output "stage-result"
  assert_output "failure"
}

@test "unknown result values normalize to skipped" {
  export PUBLISH_MAVEN_REGISTRY_RESULT="bogus"
  export BUILD_CONTAINERS_RESULT="in_progress"
  export GITHUBPACKAGES_ARTIFACTS='[{"name":"app.jar"}]'
  export CONTAINERS='[{"name":"app"}]'

  run_script "summary/write-publish-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "true"

  run get_github_output "stage-result"
  assert_output "success"
}

@test "stage-ran is true when only one artifact type is non-empty" {
  export PUBLISH_MAVEN_CENTRAL_RESULT="success"
  export MAVENCENTRAL_ARTIFACTS='[{"name":"app.jar"}]'

  run_script "summary/write-publish-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "true"

  run get_github_output "stage-result"
  assert_output "success"
}

@test "stage-ran is false when all artifacts are empty arrays" {
  export PUBLISH_MAVEN_REGISTRY_RESULT="success"
  export GITHUBPACKAGES_ARTIFACTS='[]'
  export MAVENCENTRAL_ARTIFACTS='[]'
  export XCODEIOS_ARTIFACTS='[]'
  export GOOGLEPLAY_ARTIFACTS='[]'
  export CONTAINERS='[]'

  run_script "summary/write-publish-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "false"

  run get_github_output "stage-result"
  assert_output "skipped"
}

@test "result-json contains expected structure with all targets" {
  export PUBLISH_MAVEN_REGISTRY_RESULT="success"
  export PUBLISH_MAVEN_CENTRAL_RESULT="failure"
  export PUBLISH_APPLE_APPSTORE_RESULT="skipped"
  export PUBLISH_GOOGLE_PLAY_RESULT="cancelled"
  export BUILD_CONTAINERS_RESULT="success"
  export GITHUBPACKAGES_ARTIFACTS='[{"name":"app.jar"}]'
  export MAVENCENTRAL_ARTIFACTS='[{"name":"app.jar"}]'

  run_script "summary/write-publish-stage-result.sh"
  assert_success

  run get_github_output "result-json"
  assert_output --partial '"stage":"publish"'
  assert_output --partial '"result":"failure"'
  assert_output --partial '"ran":true'
  assert_output --partial '"githubpackages":"success"'
  assert_output --partial '"mavencentral":"failure"'
  assert_output --partial '"appleappstore":"skipped"'
  assert_output --partial '"googleplay":"cancelled"'
  assert_output --partial '"containers":"success"'
}

@test "result-json ran is false when stage did not run" {
  run_script "summary/write-publish-stage-result.sh"
  assert_success

  run get_github_output "result-json"
  assert_output --partial '"ran":false'
  assert_output --partial '"result":"skipped"'
}

@test "result values without artifacts still report skipped stage" {
  export PUBLISH_MAVEN_REGISTRY_RESULT="success"
  export BUILD_CONTAINERS_RESULT="success"

  run_script "summary/write-publish-stage-result.sh"
  assert_success

  run get_github_output "stage-ran"
  assert_output "false"

  run get_github_output "stage-result"
  assert_output "skipped"
}
