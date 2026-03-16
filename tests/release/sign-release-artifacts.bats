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
  mkdir -p "${TEST_DIR}/bin" "${TEST_DIR}/release-artifacts"
  cat > "${TEST_DIR}/bin/gpg" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
target="${@: -1}"
touch "${target}.asc"
EOF
  chmod +x "${TEST_DIR}/bin/gpg"
  export PATH="${TEST_DIR}/bin:${PATH}"
}

teardown() {
  common_teardown
}

@test "sign-release-artifacts signs checksums and supported artifacts" {
  printf 'abc  file\n' > "${TEST_DIR}/checksums.sha256"
  touch "${TEST_DIR}/release-artifacts/app.jar"
  touch "${TEST_DIR}/release-artifacts/original-app.jar"

  pushd "$TEST_DIR" >/dev/null
  run_script "release/sign-release-artifacts.sh" "ABC123"
  popd >/dev/null

  assert_success
  assert_file_exist "${TEST_DIR}/checksums.sha256.asc"
  assert_file_exist "${TEST_DIR}/app.jar.asc"
  assert_file_not_exist "${TEST_DIR}/original-app.jar.asc"
}

@test "sign-release-artifacts skips missing inputs gracefully" {
  pushd "$TEST_DIR" >/dev/null
  run_script "release/sign-release-artifacts.sh" "ABC123"
  popd >/dev/null

  assert_success
}
