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
  mkdir -p "${TEST_DIR}/bin"
  cat > "${TEST_DIR}/bin/xcodebuild" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" > xcodebuild-args.txt
printf 'xcodebuild output\n'
EOF
  cat > "${TEST_DIR}/bin/xcbeautify" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
cat >/dev/null
EOF
  chmod +x "${TEST_DIR}/bin/xcodebuild" "${TEST_DIR}/bin/xcbeautify"
  export PATH="${TEST_DIR}/bin:${PATH}"
  export SCHEME="Demo"
  export CONFIGURATION="Release"
  export DESTINATION="generic/platform=iOS"
}

teardown() {
  common_teardown
}

@test "archive-app prefers workspace when provided" {
  export WORKSPACE="Demo.xcworkspace"
  export PROJECT="Demo.xcodeproj"
  export XC_CONFIG_PATH="Config.xcconfig"
  export BUILD_NUMBER="42"

  pushd "$TEST_DIR" >/dev/null
  run_script "apple/archive-app.sh"
  popd >/dev/null

  assert_success
  assert_output --partial "Running: xcodebuild archive -workspace Demo.xcworkspace"
  assert_output --partial "-xcconfig Config.xcconfig"
  assert_output --partial "CURRENT_PROJECT_VERSION=42"
}

@test "archive-app uses project when workspace is absent" {
  export WORKSPACE=""
  export PROJECT="Demo.xcodeproj"
  export XC_CONFIG_PATH=""
  export BUILD_NUMBER=""

  pushd "$TEST_DIR" >/dev/null
  run_script "apple/archive-app.sh"
  popd >/dev/null

  assert_success
  assert_output --partial "Running: xcodebuild archive -project Demo.xcodeproj"
}
