#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/../test_helper.bash"

# =============================================================================
# Setup / Teardown
# =============================================================================

setup() {
  common_setup
  EXTRACT_DIR="${TEST_DIR}/extracted-binaries"
  mkdir -p "$EXTRACT_DIR"
}

teardown() {
  common_teardown
}

# =============================================================================
# Helper
# =============================================================================

run_suffix() {
  run_script "container/suffix-extracted-binaries.sh" "$@"
}

# =============================================================================
# EXPECTED_NAMES path
# =============================================================================

@test "suffix renames the listed binary on amd64" {
  printf "fake-binary-content" >"$EXTRACT_DIR/hsm-worker"

  run_suffix "$EXTRACT_DIR" "amd64" "hsm-worker"

  assert_success
  assert_file_exist "$EXTRACT_DIR/hsm-worker-linux-amd64"
  assert_file_not_exist "$EXTRACT_DIR/hsm-worker"
}

@test "suffix renames the listed binary on arm64" {
  printf "fake" >"$EXTRACT_DIR/wallet-bff"

  run_suffix "$EXTRACT_DIR" "arm64" "wallet-bff"

  assert_success
  assert_file_exist "$EXTRACT_DIR/wallet-bff-linux-arm64"
}

@test "suffix renames each name in a comma-separated list" {
  printf "a" >"$EXTRACT_DIR/hsm-worker"
  printf "b" >"$EXTRACT_DIR/digg-hsm-keytool"

  run_suffix "$EXTRACT_DIR" "amd64" "hsm-worker,digg-hsm-keytool"

  assert_success
  assert_file_exist "$EXTRACT_DIR/hsm-worker-linux-amd64"
  assert_file_exist "$EXTRACT_DIR/digg-hsm-keytool-linux-amd64"
}

@test "suffix tolerates whitespace around comma-separated names" {
  printf "a" >"$EXTRACT_DIR/hsm-worker"
  printf "b" >"$EXTRACT_DIR/digg-hsm-keytool"

  run_suffix "$EXTRACT_DIR" "amd64" "hsm-worker,  digg-hsm-keytool"

  assert_success
  assert_file_exist "$EXTRACT_DIR/hsm-worker-linux-amd64"
  assert_file_exist "$EXTRACT_DIR/digg-hsm-keytool-linux-amd64"
}

@test "suffix skips a listed name that doesn't exist on disk" {
  printf "a" >"$EXTRACT_DIR/hsm-worker"

  run_suffix "$EXTRACT_DIR" "amd64" "hsm-worker,not-built"

  assert_success
  assert_file_exist "$EXTRACT_DIR/hsm-worker-linux-amd64"
  assert_file_not_exist "$EXTRACT_DIR/not-built-linux-amd64"
}

# =============================================================================
# Default path (no EXPECTED_NAMES)
# =============================================================================

@test "suffix renames every top-level file when EXPECTED_NAMES is empty" {
  printf "a" >"$EXTRACT_DIR/hsm-worker"
  printf "b" >"$EXTRACT_DIR/wallet-bff"

  run_suffix "$EXTRACT_DIR" "amd64"

  assert_success
  assert_file_exist "$EXTRACT_DIR/hsm-worker-linux-amd64"
  assert_file_exist "$EXTRACT_DIR/wallet-bff-linux-amd64"
}

@test "suffix renames every top-level file when EXPECTED_NAMES is the empty string" {
  printf "a" >"$EXTRACT_DIR/hsm-worker"

  run_suffix "$EXTRACT_DIR" "arm64" ""

  assert_success
  assert_file_exist "$EXTRACT_DIR/hsm-worker-linux-arm64"
}

@test "suffix leaves nested files untouched in default mode" {
  mkdir -p "$EXTRACT_DIR/nested"
  printf "deep" >"$EXTRACT_DIR/nested/keepme"
  printf "top" >"$EXTRACT_DIR/hsm-worker"

  run_suffix "$EXTRACT_DIR" "amd64"

  assert_success
  assert_file_exist "$EXTRACT_DIR/hsm-worker-linux-amd64"
  assert_file_exist "$EXTRACT_DIR/nested/keepme"
  assert_file_not_exist "$EXTRACT_DIR/nested/keepme-linux-amd64"
}

# =============================================================================
# Errors
# =============================================================================

@test "suffix fails when the directory does not exist" {
  run_suffix "$TEST_DIR/missing-dir" "amd64"

  assert_failure
  assert_stderr --partial "directory not found"
}

@test "suffix is a no-op when the directory is empty" {
  run_suffix "$EXTRACT_DIR" "amd64"

  assert_success
  # No files to rename, no error.
}
