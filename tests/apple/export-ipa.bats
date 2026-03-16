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
  mkdir -p "${TEST_DIR}/bin" "${TEST_DIR}/build"
  cat > "${TEST_DIR}/bin/xcodebuild" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" > xcodebuild-export-args.txt
printf 'exported\n'
EOF
  cat > "${TEST_DIR}/bin/xcbeautify" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
cat >/dev/null
EOF
  chmod +x "${TEST_DIR}/bin/xcodebuild" "${TEST_DIR}/bin/xcbeautify"
  export PATH="${TEST_DIR}/bin:${PATH}"
  export EXPORT_OPTIONS_VAR="IOS_EXPORT_OPTIONS"
}

teardown() {
  common_teardown
}

@test "export-ipa decodes export options and calls xcodebuild" {
  export EXPORT_OPTIONS_BASE64="cGxpc3Q="

  pushd "$TEST_DIR" >/dev/null
  run_script "apple/export-ipa.sh"
  popd >/dev/null

  assert_success
  assert_file_exist "${TEST_DIR}/export-options.plist"
  assert_file_contains "${TEST_DIR}/xcodebuild-export-args.txt" "-exportArchive"
}

@test "export-ipa fails when export options are missing" {
  export EXPORT_OPTIONS_BASE64=""

  pushd "$TEST_DIR" >/dev/null
  run_script "apple/export-ipa.sh"
  popd >/dev/null

  assert_failure
  assert_output --partial "Export options not found in variable IOS_EXPORT_OPTIONS"
}
