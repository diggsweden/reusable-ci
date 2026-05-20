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
  mkdir -p "${TEST_DIR}/release-bin"
  cat > "${TEST_DIR}/release-bin/gpg" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
target="${@: -1}"
touch "${target}.asc"
EOF
  chmod +x "${TEST_DIR}/release-bin/gpg"
  export PATH="${TEST_DIR}/release-bin:${PATH}"
  # Create an SBOM so the ZIP gets created
  printf '{"spdx": "test"}' > "${TEST_DIR}/myapp-pom-sbom.spdx.json"
}

teardown() {
  common_teardown
}

@test "create-sbom-zip signs the zip when SIGN_ARTIFACTS=true" {
  export SIGN_ARTIFACTS="true"
  export GPG_KEY_ID="ABC123"

  pushd "$TEST_DIR" >/dev/null
  run_script "release/create-sbom-zip.sh" "myapp" "v1.2.3"
  popd >/dev/null

  assert_success
  assert_file_exist "${TEST_DIR}/myapp-1.2.3-sboms.zip"
  assert_file_exist "${TEST_DIR}/myapp-1.2.3-sboms.zip.asc"
}

@test "create-sbom-zip skips signing when SIGN_ARTIFACTS=false" {
  export SIGN_ARTIFACTS="false"
  export GPG_KEY_ID="ABC123"

  pushd "$TEST_DIR" >/dev/null
  run_script "release/create-sbom-zip.sh" "myapp" "v1.2.3"
  popd >/dev/null

  assert_success
  assert_file_exist "${TEST_DIR}/myapp-1.2.3-sboms.zip"
  assert_file_not_exist "${TEST_DIR}/myapp-1.2.3-sboms.zip.asc"
}

@test "create-sbom-zip skips signing when GPG_KEY_ID is empty" {
  export SIGN_ARTIFACTS="true"
  export GPG_KEY_ID=""

  pushd "$TEST_DIR" >/dev/null
  run_script "release/create-sbom-zip.sh" "myapp" "v1.2.3"
  popd >/dev/null

  assert_success
  assert_file_exist "${TEST_DIR}/myapp-1.2.3-sboms.zip"
  assert_file_not_exist "${TEST_DIR}/myapp-1.2.3-sboms.zip.asc"
}

@test "create-sbom-zip skips signing when env vars not set" {
  pushd "$TEST_DIR" >/dev/null
  run_script "release/create-sbom-zip.sh" "myapp" "v1.2.3"
  popd >/dev/null

  assert_success
  assert_file_exist "${TEST_DIR}/myapp-1.2.3-sboms.zip"
  assert_file_not_exist "${TEST_DIR}/myapp-1.2.3-sboms.zip.asc"
}
