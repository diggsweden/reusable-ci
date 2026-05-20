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

@test "extract-npm-tarball extracts archive contents" {
  mkdir -p "${TEST_DIR}/package"
  printf 'hello\n' > "${TEST_DIR}/package/file.txt"
  tar -czf "${TEST_DIR}/package.tgz" -C "${TEST_DIR}" package

  pushd "$TEST_DIR" >/dev/null
  run_script "container/extract-npm-tarball.sh"
  popd >/dev/null

  assert_success
  assert_file_exist "${TEST_DIR}/file.txt"
  assert_file_not_exist "${TEST_DIR}/package.tgz"
}
