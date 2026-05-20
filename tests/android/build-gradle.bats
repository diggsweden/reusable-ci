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
  cat > "${TEST_DIR}/gradlew" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" > gradlew-args.txt
EOF
  chmod +x "${TEST_DIR}/gradlew"
}

teardown() {
  common_teardown
}

@test "build-gradle adds -x test when tests are skipped" {
  export SKIP_TESTS="true"
  export GRADLE_TASKS="assembleRelease bundleRelease"

  pushd "$TEST_DIR" >/dev/null
  run_script "android/build-gradle.sh"
  popd >/dev/null

  assert_success
  assert_file_contains "${TEST_DIR}/gradlew-args.txt" "assembleRelease bundleRelease -x test"
}

@test "build-gradle runs tasks directly when tests are enabled" {
  export SKIP_TESTS="false"
  export GRADLE_TASKS="assembleDebug"

  pushd "$TEST_DIR" >/dev/null
  run_script "android/build-gradle.sh"
  popd >/dev/null

  assert_success
  assert_file_contains "${TEST_DIR}/gradlew-args.txt" "assembleDebug"
}
