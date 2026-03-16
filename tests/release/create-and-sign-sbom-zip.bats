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
  mkdir -p "${TEST_DIR}/release-bin" "${TEST_DIR}/release-scripts"
  cat > "${TEST_DIR}/release-scripts/create-sbom-zip.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
touch "$1-${2#v}-sboms.zip"
EOF
  chmod +x "${TEST_DIR}/release-scripts/create-sbom-zip.sh"
  cat > "${TEST_DIR}/release-bin/gpg" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
target="${@: -1}"
touch "${target}.asc"
EOF
  chmod +x "${TEST_DIR}/release-bin/gpg"
  export PATH="${TEST_DIR}/release-bin:${PATH}"
}

teardown() {
  common_teardown
}

@test "create-and-sign-sbom-zip creates and signs the zip when enabled" {
  export PROJECT_NAME="demo"
  export VERSION="v1.2.3"
  export VERSION_NO_V="1.2.3"
  export SIGN_ARTIFACTS="true"
  export GPG_KEY_ID="ABC123"
  export RELEASE_SCRIPT_ROOT="${TEST_DIR}/release-scripts"

  pushd "$TEST_DIR" >/dev/null
  run_script "release/create-and-sign-sbom-zip.sh"
  popd >/dev/null

  assert_success
  assert_file_exist "${TEST_DIR}/demo-1.2.3-sboms.zip"
  assert_file_exist "${TEST_DIR}/demo-1.2.3-sboms.zip.asc"
}

@test "create-and-sign-sbom-zip skips signing when disabled" {
  export PROJECT_NAME="demo"
  export VERSION="v1.2.3"
  export VERSION_NO_V="1.2.3"
  export SIGN_ARTIFACTS="false"
  export GPG_KEY_ID="ABC123"
  export RELEASE_SCRIPT_ROOT="${TEST_DIR}/release-scripts"

  pushd "$TEST_DIR" >/dev/null
  run_script "release/create-and-sign-sbom-zip.sh"
  popd >/dev/null

  assert_success
  assert_file_exist "${TEST_DIR}/demo-1.2.3-sboms.zip"
  assert_file_not_exist "${TEST_DIR}/demo-1.2.3-sboms.zip.asc"
}
