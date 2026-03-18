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
  export SOURCE_FILE="ReleasenotesTmp"
  export TARGET_FILE="release-notes.md"
  export RELEASE_VERSION=""
  export RELEASE_COMMIT=""
}

teardown() {
  common_teardown
}

@test "prepare-release-notes copies generated notes when present" {
  printf 'hello notes\n' > "${TEST_DIR}/ReleasenotesTmp"

  pushd "$TEST_DIR" >/dev/null
  run_script "release/prepare-release-notes.sh"
  popd >/dev/null

  assert_success
  assert_file_exist "${TEST_DIR}/release-notes.md"
  assert_file_contains "${TEST_DIR}/release-notes.md" "hello notes"
}

@test "prepare-release-notes creates empty target when source is missing and no version" {
  pushd "$TEST_DIR" >/dev/null
  run_script "release/prepare-release-notes.sh"
  popd >/dev/null

  assert_success
  assert_file_exist "${TEST_DIR}/release-notes.md"
  assert_output --partial "No release notes generated"
}

@test "prepare-release-notes creates fallback with version when source is missing" {
  export RELEASE_VERSION="v1.2.3"
  export RELEASE_COMMIT="abc123def"

  pushd "$TEST_DIR" >/dev/null
  run_script "release/prepare-release-notes.sh"
  popd >/dev/null

  assert_success
  assert_file_exist "${TEST_DIR}/release-notes.md"
  assert_output --partial "creating fallback"
  assert_file_contains "${TEST_DIR}/release-notes.md" "Release v1.2.3"
  assert_file_contains "${TEST_DIR}/release-notes.md" "abc123def"
}

@test "prepare-release-notes reports file size when source exists" {
  printf 'hello notes\n' > "${TEST_DIR}/ReleasenotesTmp"
  export RELEASE_VERSION="v1.0.0"
  export RELEASE_COMMIT="sha123"

  pushd "$TEST_DIR" >/dev/null
  run_script "release/prepare-release-notes.sh"
  popd >/dev/null

  assert_success
  assert_output --partial "Changelog artifact found"
}
