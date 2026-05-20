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
  export IMAGE_NAME="ghcr.io/diggsweden/demo"
  export IMAGE_DIGEST="sha256:abc123"
  export IMAGE_METADATA='{"tags":["latest"]}'
}

teardown() {
  common_teardown
}

@test "write-image-details writes image output and logs details" {
  run_script "container/write-image-details.sh"

  assert_success
  assert_output --partial "Digest: sha256:abc123"
  run get_github_output image
  assert_output "ghcr.io/diggsweden/demo"
}
