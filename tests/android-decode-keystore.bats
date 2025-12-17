#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/test_helper.bash"

# =============================================================================
# Setup / Teardown
# =============================================================================

setup() {
  common_setup
}

teardown() {
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

run_decode_keystore() {
  run --separate-stderr bash "$SCRIPTS_DIR/android/decode-keystore.sh"
  debug_output
}

# Create a fake keystore and base64 encode it
create_test_keystore() {
  local keystore_content="fake-keystore-content-for-testing"
  printf '%s' "$keystore_content" | base64
}

# =============================================================================
# Decode Keystore Tests
# =============================================================================

@test "decode-keystore.sh fails when ANDROID_KEYSTORE_BASE64 is not set" {
  unset ANDROID_KEYSTORE_BASE64

  run_decode_keystore

  assert_failure
  assert_output --partial "ANDROID_KEYSTORE secret not found"
}

@test "decode-keystore.sh fails when ANDROID_KEYSTORE_BASE64 is empty" {
  export ANDROID_KEYSTORE_BASE64=""

  run_decode_keystore

  assert_failure
  assert_output --partial "ANDROID_KEYSTORE secret not found"
}

@test "decode-keystore.sh decodes keystore successfully" {
  export ANDROID_KEYSTORE_BASE64="$(create_test_keystore)"

  run_decode_keystore

  assert_success
  assert_file_exists "$TEST_DIR/release.keystore"
  
  # Verify the keystore content was decoded correctly
  local decoded_content
  decoded_content=$(cat "$TEST_DIR/release.keystore")
  assert_equal "$decoded_content" "fake-keystore-content-for-testing"
}

@test "decode-keystore.sh outputs ANDROID_KEYSTORE_PATH" {
  export ANDROID_KEYSTORE_BASE64="$(create_test_keystore)"

  run_decode_keystore

  assert_success
  # The script outputs ANDROID_KEYSTORE_PATH=<path> for GITHUB_ENV
  assert_output --partial "ANDROID_KEYSTORE_PATH="
  assert_output --partial "/release.keystore"
}

@test "decode-keystore.sh outputs success message to stderr" {
  export ANDROID_KEYSTORE_BASE64="$(create_test_keystore)"

  run_decode_keystore

  assert_success
  # Success message goes to stderr (for logging)
  assert_equal "$stderr" "✓ Android keystore decoded successfully"
}

@test "decode-keystore.sh outputs absolute path" {
  export ANDROID_KEYSTORE_BASE64="$(create_test_keystore)"

  run_decode_keystore

  assert_success
  # Path should be absolute (start with /)
  assert_output --regexp "ANDROID_KEYSTORE_PATH=/.*/release.keystore"
}
