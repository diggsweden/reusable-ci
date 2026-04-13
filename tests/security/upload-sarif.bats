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
  setup_github_env

  export GITHUB_REPOSITORY="diggsweden/test-repo"
  export GITHUB_SHA="abc1234567890"
  export GITHUB_REF="refs/heads/main"

  # Create a minimal SARIF file
  cat > "$TEST_DIR/results.sarif" << 'SARIF'
{
  "version": "2.1.0",
  "$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
  "runs": []
}
SARIF

  export SARIF_FILE="$TEST_DIR/results.sarif"
}

teardown() {
  common_teardown
}

# =============================================================================
# Helper
# =============================================================================

run_upload_sarif() {
  run bash "$SCRIPTS_DIR/security/upload-sarif.sh"
}

# =============================================================================
# Tests: Token not set
# =============================================================================

@test "skips upload when SARIF_UPLOAD_TOKEN is empty" {
  unset SARIF_UPLOAD_TOKEN

  run_upload_sarif

  assert_success
  assert_output --partial "SARIF upload to Code Scanning skipped"
}

@test "skips upload when SARIF_UPLOAD_TOKEN is unset" {
  export SARIF_UPLOAD_TOKEN=""

  run_upload_sarif

  assert_success
  assert_output --partial "SARIF upload to Code Scanning skipped"
}

# =============================================================================
# Tests: Validation
# =============================================================================

@test "fails when SARIF_FILE is empty" {
  export SARIF_UPLOAD_TOKEN="test-token"
  export SARIF_FILE=""

  run_upload_sarif

  assert_failure
  assert_output --partial "SARIF_FILE environment variable is required"
}

@test "skips when SARIF file does not exist" {
  export SARIF_UPLOAD_TOKEN="test-token"
  export SARIF_FILE="$TEST_DIR/nonexistent.sarif"

  run_upload_sarif

  assert_success
  assert_output --partial "SARIF file not found"
}

@test "fails when GITHUB_REPOSITORY is empty" {
  export SARIF_UPLOAD_TOKEN="test-token"
  export GITHUB_REPOSITORY=""

  run_upload_sarif

  assert_failure
  assert_output --partial "GITHUB_REPOSITORY environment variable is required"
}

@test "fails when GITHUB_SHA is empty" {
  export SARIF_UPLOAD_TOKEN="test-token"
  export GITHUB_SHA=""

  run_upload_sarif

  assert_failure
  assert_output --partial "GITHUB_SHA environment variable is required"
}

@test "fails when GITHUB_REF is empty" {
  export SARIF_UPLOAD_TOKEN="test-token"
  export GITHUB_REF=""

  run_upload_sarif

  assert_failure
  assert_output --partial "GITHUB_REF environment variable is required"
}

# =============================================================================
# Tests: Upload
# =============================================================================

@test "uploads SARIF when token and file are present" {
  export SARIF_UPLOAD_TOKEN="test-token"
  export GITHUB_API_URL="http://localhost:9999"

  # Stub curl to capture stdin payload and simulate 202 Accepted
  create_mock_binary "curl" '
    payload="$(cat)"
    printf "%s" "$payload" > '"$TEST_DIR"'/captured-payload.json
    printf "ok\n202"
  '
  use_mock_path

  run_upload_sarif

  assert_success
  assert_output --partial "Uploading"
  assert_output --partial "SARIF uploaded successfully (HTTP 202)"

  # Verify the payload is valid JSON with expected fields
  assert_file_exist "$TEST_DIR/captured-payload.json"
  run jq -e '.commit_sha' "$TEST_DIR/captured-payload.json"
  assert_success
  assert_output '"abc1234567890"'
  run jq -e '.ref' "$TEST_DIR/captured-payload.json"
  assert_success
  assert_output '"refs/heads/main"'
  run jq -e '.sarif' "$TEST_DIR/captured-payload.json"
  assert_success
}

@test "includes tool_name when SARIF_CATEGORY is set" {
  export SARIF_UPLOAD_TOKEN="test-token"
  export SARIF_CATEGORY="opengrep-sast"
  export GITHUB_API_URL="http://localhost:9999"

  create_mock_binary "curl" '
    payload="$(cat)"
    printf "%s" "$payload" > '"$TEST_DIR"'/captured-payload.json
    printf "ok\n202"
  '
  use_mock_path

  run_upload_sarif

  assert_success
  assert_output --partial "[opengrep-sast]"

  run jq -e '.tool_name' "$TEST_DIR/captured-payload.json"
  assert_success
  assert_output '"opengrep-sast"'
}

@test "handles large SARIF files without ARG_MAX errors" {
  export SARIF_UPLOAD_TOKEN="test-token"
  export GITHUB_API_URL="http://localhost:9999"

  # Generate a SARIF file large enough to exceed typical ARG_MAX (~128KB)
  python3 -c "
import json
runs = [{'results': [{'message': {'text': 'x' * 200}} for _ in range(500)]}]
print(json.dumps({'version': '2.1.0', 'runs': runs}))
" > "$TEST_DIR/results.sarif"
  export SARIF_FILE="$TEST_DIR/results.sarif"

  create_mock_binary "curl" 'cat > /dev/null; printf "ok\n202"'
  use_mock_path

  run_upload_sarif

  assert_success
  assert_output --partial "SARIF uploaded successfully (HTTP 202)"
}

@test "fails on non-2xx response" {
  export SARIF_UPLOAD_TOKEN="test-token"
  export GITHUB_API_URL="http://localhost:9999"

  # Stub curl to simulate 401 Unauthorized
  create_mock_binary "curl" 'cat > /dev/null; printf "{\"message\":\"Bad credentials\"}\n401"'
  use_mock_path

  run_upload_sarif

  assert_failure
  assert_output --partial "SARIF upload failed (HTTP 401)"
}

# =============================================================================
# Tests: Platform-aware logging
# =============================================================================

@test "uses GitHub Actions annotation when GITHUB_ACTIONS is true" {
  unset SARIF_UPLOAD_TOKEN
  export GITHUB_ACTIONS="true"

  run_upload_sarif

  assert_success
  assert_output --partial "::notice::"
}

@test "uses plain text when not in GitHub Actions" {
  unset SARIF_UPLOAD_TOKEN
  unset GITHUB_ACTIONS

  run_upload_sarif

  assert_success
  assert_output --partial "NOTICE:"
}
