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

@test "read-minimal-changelog writes multiline content to GITHUB_OUTPUT" {
  cat > "${TEST_DIR}/minimal-changelog.txt" <<'EOF'
Line one
Line two
EOF

  run_script "version/read-minimal-changelog.sh" "${TEST_DIR}/minimal-changelog.txt"

  assert_success
  assert_file_contains "$GITHUB_OUTPUT" "content<<EOF"
  assert_file_contains "$GITHUB_OUTPUT" "Line one"
  assert_file_contains "$GITHUB_OUTPUT" "Line two"
}

@test "read-minimal-changelog falls back when file is missing" {
  run_script "version/read-minimal-changelog.sh" "${TEST_DIR}/missing.txt"

  assert_success
  run get_github_output content
  assert_output "No changes for this release"
}
