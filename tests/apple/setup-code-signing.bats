#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
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
  export RUNNER_TEMP="${TEST_DIR}/runner-temp"
  mkdir -p "$RUNNER_TEMP"

  # Override HOME to isolate ~/Library writes
  export HOME="${TEST_DIR}/home"
  mkdir -p "$HOME"

  # Create mock security command
  create_mock_binary "security" 'printf "security: %s\n" "$*"'

  # Create mock base64 that creates the output file when -o is used
  create_mock_binary "base64" '
prev=""
for arg in "$@"; do
  if [[ "$prev" == "-o" ]]; then
    printf "decoded" > "$arg"
  fi
  prev="$arg"
done
'
  use_mock_path
}

teardown() {
  common_teardown
}

# =============================================================================
# Validation - Missing Secrets
# =============================================================================

@test "setup-code-signing fails when CERTIFICATE_BASE64 is empty" {
  export CERTIFICATE_BASE64=""
  export PROVISIONING_PROFILE_BASE64="dGVzdA=="
  export KEYCHAIN_PASSWORD="test-password"

  run_script "apple/setup-code-signing.sh"

  assert_failure
  assert_output --partial "CERTIFICATE_BASE64"
}

@test "setup-code-signing fails when PROVISIONING_PROFILE_BASE64 is empty" {
  export CERTIFICATE_BASE64="dGVzdA=="
  export PROVISIONING_PROFILE_BASE64=""
  export KEYCHAIN_PASSWORD="test-password"

  run_script "apple/setup-code-signing.sh"

  assert_failure
  assert_output --partial "PROVISIONING_PROFILE_BASE64"
}

@test "setup-code-signing fails when KEYCHAIN_PASSWORD is empty" {
  export CERTIFICATE_BASE64="dGVzdA=="
  export PROVISIONING_PROFILE_BASE64="dGVzdA=="
  export KEYCHAIN_PASSWORD=""

  run_script "apple/setup-code-signing.sh"

  assert_failure
  assert_output --partial "KEYCHAIN_PASSWORD"
}

# =============================================================================
# Successful Configuration
# =============================================================================

@test "setup-code-signing succeeds when all secrets are set" {
  export CERTIFICATE_BASE64="dGVzdA=="
  export PROVISIONING_PROFILE_BASE64="dGVzdA=="
  export KEYCHAIN_PASSWORD="test-password"
  export CERTIFICATE_PASSPHRASE="test-phrase"

  run_script "apple/setup-code-signing.sh"

  assert_success
}

@test "setup-code-signing shows success message when configured" {
  export CERTIFICATE_BASE64="dGVzdA=="
  export PROVISIONING_PROFILE_BASE64="dGVzdA=="
  export KEYCHAIN_PASSWORD="test-password"
  export CERTIFICATE_PASSPHRASE="test-phrase"

  run_script "apple/setup-code-signing.sh"

  assert_success
  assert_output --partial "Code signing configured successfully"
}

@test "setup-code-signing creates provisioning profiles directory" {
  export CERTIFICATE_BASE64="dGVzdA=="
  export PROVISIONING_PROFILE_BASE64="dGVzdA=="
  export KEYCHAIN_PASSWORD="test-password"
  export CERTIFICATE_PASSPHRASE="test-phrase"

  run_script "apple/setup-code-signing.sh"

  assert_success
  assert_dir_exists "${HOME}/Library/MobileDevice/Provisioning Profiles"
}

@test "setup-code-signing calls security to create keychain" {
  export CERTIFICATE_BASE64="dGVzdA=="
  export PROVISIONING_PROFILE_BASE64="dGVzdA=="
  export KEYCHAIN_PASSWORD="test-password"
  export CERTIFICATE_PASSPHRASE="test-phrase"

  run_script "apple/setup-code-signing.sh"

  assert_success
  assert_output --partial "security: create-keychain"
}
