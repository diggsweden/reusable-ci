#!/usr/bin/env bats

# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/../test_helper.bash"

# Wrapper is one of the few bash files with no native unit coverage; these
# tests lock in the contract (required env, missing gradlew, init script content)
# without actually invoking Gradle.

SCRIPT="scripts/sbom/build/gradle/generate-gradle-sbom.sh"

setup() {
  common_setup
}

teardown() {
  common_teardown
}

@test "generate-gradle-sbom errors when CYCLONEDX_GRADLE_VERSION is unset" {
  run bash "${TESTS_DIR}/../${SCRIPT}"
  assert_failure
  assert_output --partial "CYCLONEDX_GRADLE_VERSION is required"
}

@test "generate-gradle-sbom errors when gradlew is missing" {
  export CYCLONEDX_GRADLE_VERSION="3.2.1"
  run bash "${TESTS_DIR}/../${SCRIPT}"
  assert_failure
  assert_output --partial "gradlew not found"
}

@test "generate-gradle-sbom invokes gradlew with the init script and cyclonedxBom" {
  # Stub gradlew to record its invocation, confirming the wrapper passes
  # the right flags to a real Gradle without us needing Gradle installed.
  cat >gradlew <<'EOF'
#!/usr/bin/env bash
printf 'args=%s\n' "$*" >gradlew.log
# Copy the init script so the test can inspect its contents.
for ((i=1; i<=$#; i++)); do
  if [[ "${!i}" == "--init-script" ]]; then
    next=$((i+1))
    cp "${!next}" init-script.captured
  fi
done
exit 0
EOF
  chmod +x gradlew

  export CYCLONEDX_GRADLE_VERSION="3.2.1"
  run bash "${TESTS_DIR}/../${SCRIPT}"

  assert_success
  # Confirm the invocation shape.
  assert_file_exists "gradlew.log"
  grep -q -- "--init-script" gradlew.log
  grep -q "cyclonedxBom" gradlew.log
  # Confirm the pinned plugin version flows into the init script.
  assert_file_exists "init-script.captured"
  grep -q '3\.2\.1' init-script.captured
  grep -q 'CyclonedxPlugin' init-script.captured
}

@test "generate-gradle-sbom removes its temp init script on exit" {
  cat >gradlew <<'EOF'
#!/usr/bin/env bash
for ((i=1; i<=$#; i++)); do
  if [[ "${!i}" == "--init-script" ]]; then
    next=$((i+1))
    printf '%s' "${!next}" >init-path.captured
  fi
done
exit 0
EOF
  chmod +x gradlew

  export CYCLONEDX_GRADLE_VERSION="3.2.1"
  run bash "${TESTS_DIR}/../${SCRIPT}"
  assert_success

  assert_file_exists "init-path.captured"
  init_path=$(cat init-path.captured)
  # The trap should have cleaned up even on success.
  [[ ! -f "$init_path" ]]
}
