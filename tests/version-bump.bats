#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/libs/bats-mock/stub.bash"
load "${BATS_TEST_DIRNAME}/test_helper.bash"

# =============================================================================
# Setup / Teardown
# =============================================================================

setup() {
  common_setup
}

teardown() {
  unstub mvn 2>/dev/null || true
  unstub npm 2>/dev/null || true
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

# Run bump-version with debug output
run_bump_version() {
  run_script "version/bump-version.sh" "$@"
}

# Create a gradle.properties file with default or custom content
create_gradle_properties() {
  local version="${1:-0.0.1}"
  local version_code="${2:-1}"
  cat > "$TEST_DIR/gradle.properties" << EOF
versionName=$version
versionCode=$version_code
EOF
}

# Create a gradle.properties file without versionCode
create_gradle_properties_no_code() {
  local version="${1:-0.0.1}"
  cat > "$TEST_DIR/gradle.properties" << EOF
versionName=$version
EOF
}

# Create an xcconfig file
create_xcconfig() {
  local version="${1:-0.9.0}"
  cat > "$TEST_DIR/versions.xcconfig" << EOF
MARKETING_VERSION = $version
EOF
}

# =============================================================================
# Input Validation Tests
# =============================================================================

@test "bump-version.sh fails on unknown project type" {
  run_bump_version "unknown-type" "1.0.0" "$TEST_DIR"

  assert_failure
  assert_output --partial "Unknown project type"
}

@test "bump-version.sh fails on missing arguments" {
  run_bump_version

  assert_failure
  # Script fails with unbound variable error when args missing
  [[ "$stderr" == *"unbound variable"* ]] || [[ "$stderr" == *"Usage"* ]]
}

@test "bump-version.sh fails with only project type" {
  run_bump_version "maven"

  assert_failure
  # Script fails with unbound variable error when args missing
  [[ "$stderr" == *"unbound variable"* ]] || [[ "$stderr" == *"Usage"* ]]
}

# =============================================================================
# Gradle Project Tests
# =============================================================================

@test "bump-version.sh updates gradle.properties versionName" {
  create_gradle_properties

  run_bump_version "gradle" "2.0.0" "$TEST_DIR" "gradle.properties"

  assert_success
  run cat "$TEST_DIR/gradle.properties"
  assert_output --partial "versionName=2.0.0"
}

@test "bump-version.sh increments gradle versionCode" {
  create_gradle_properties "0.0.1" "5"

  run_bump_version "gradle" "2.0.0" "$TEST_DIR" "gradle.properties"

  assert_success
  run cat "$TEST_DIR/gradle.properties"
  assert_output --partial "versionCode=6"
}

@test "bump-version.sh adds versionCode if missing" {
  create_gradle_properties_no_code

  run_bump_version "gradle" "2.0.0" "$TEST_DIR" "gradle.properties"

  assert_success
  run cat "$TEST_DIR/gradle.properties"
  assert_output --partial "versionCode=1"
}

@test "bump-version.sh fails when gradle.properties missing" {
  run_bump_version "gradle" "2.0.0" "$TEST_DIR" "gradle.properties"

  assert_failure
  assert_output --partial "not found"
}

@test "bump-version.sh preserves other gradle properties" {
  cat > "$TEST_DIR/gradle.properties" << 'EOF'
versionName=0.0.1
versionCode=1
customProperty=value
anotherProperty=123
EOF

  run_bump_version "gradle" "2.0.0" "$TEST_DIR" "gradle.properties"

  assert_success
  run cat "$TEST_DIR/gradle.properties"
  assert_output --partial "customProperty=value"
  assert_output --partial "anotherProperty=123"
}

# =============================================================================
# Gradle Android Tests
# =============================================================================

@test "bump-version.sh handles gradle-android type" {
  create_gradle_properties "1.0.0" "10"

  run_bump_version "gradle-android" "1.1.0" "$TEST_DIR" "gradle.properties"

  assert_success
  run cat "$TEST_DIR/gradle.properties"
  assert_output --partial "versionName=1.1.0"
  assert_output --partial "versionCode=11"
}

@test "bump-version.sh handles large versionCode for gradle-android" {
  create_gradle_properties "1.0.0" "999"

  run_bump_version "gradle-android" "2.0.0" "$TEST_DIR" "gradle.properties"

  assert_success
  run cat "$TEST_DIR/gradle.properties"
  assert_output --partial "versionCode=1000"
}

# =============================================================================
# Maven Project Tests
# =============================================================================

@test "bump-version.sh calls mvn for maven projects" {
  stub mvn "versions:set -DnewVersion=1.0.0 -DgenerateBackupPoms=false -DprocessAllModules=true -DskipTests : true"

  run_bump_version "maven" "1.0.0" "$TEST_DIR"

  assert_success
  assert_output --partial "Maven version updated"
  unstub mvn
}

@test "bump-version.sh fails when mvn fails" {
  stub mvn "versions:set -DnewVersion=1.0.0 -DgenerateBackupPoms=false -DprocessAllModules=true -DskipTests : exit 1"

  run_bump_version "maven" "1.0.0" "$TEST_DIR"

  assert_failure
  unstub mvn
}

# =============================================================================
# NPM Project Tests
# =============================================================================

@test "bump-version.sh calls npm version for npm projects" {
  stub npm "version 1.0.0 --no-git-tag-version --allow-same-version : true"

  run_bump_version "npm" "1.0.0" "$TEST_DIR"

  assert_success
  assert_output --partial "NPM version updated"
  unstub npm
}

@test "bump-version.sh fails when npm fails" {
  stub npm "version 1.0.0 --no-git-tag-version --allow-same-version : exit 1"

  run_bump_version "npm" "1.0.0" "$TEST_DIR"

  assert_failure
  unstub npm
}

# =============================================================================
# Xcode iOS Tests
# =============================================================================

@test "bump-version.sh creates xcconfig file if missing for xcode-ios" {
  run_bump_version "xcode-ios" "1.0.0" "$TEST_DIR"

  assert_success
  assert_file_exists "$TEST_DIR/versions.xcconfig"
  run cat "$TEST_DIR/versions.xcconfig"
  assert_output --partial "MARKETING_VERSION = 1.0.0"
}

@test "bump-version.sh updates existing xcconfig for xcode-ios" {
  create_xcconfig "0.9.0"

  run_bump_version "xcode-ios" "1.0.0" "$TEST_DIR"

  assert_success
  run cat "$TEST_DIR/versions.xcconfig"
  assert_output --partial "MARKETING_VERSION = 1.0.0"
  refute_output --partial "0.9.0"
}

@test "bump-version.sh creates default xcconfig for xcode-ios ignoring custom name" {
  # The script always creates versions.xcconfig regardless of the filename arg
  cat > "$TEST_DIR/custom.xcconfig" << 'EOF'
MARKETING_VERSION = 0.5.0
EOF

  run_bump_version "xcode-ios" "1.0.0" "$TEST_DIR" "custom.xcconfig"

  assert_success
  # Script creates versions.xcconfig not the custom filename
  assert_file_exists "$TEST_DIR/versions.xcconfig"
  run cat "$TEST_DIR/versions.xcconfig"
  assert_output --partial "MARKETING_VERSION = 1.0.0"
}

# =============================================================================
# Meta Project Tests
# =============================================================================

@test "bump-version.sh handles meta project type" {
  run_bump_version "meta" "1.0.0" "$TEST_DIR"

  assert_success
  assert_output --partial "Meta project type"
  assert_output --partial "changelog generation only"
}

@test "bump-version.sh meta type does not create files" {
  run_bump_version "meta" "1.0.0" "$TEST_DIR"

  assert_success
  # Should not create any version files
  assert_file_not_exists "$TEST_DIR/gradle.properties"
  assert_file_not_exists "$TEST_DIR/versions.xcconfig"
  assert_file_not_exists "$TEST_DIR/package.json"
}

# =============================================================================
# Output Tests
# =============================================================================

@test "bump-version.sh shows version being set" {
  create_gradle_properties

  run_bump_version "gradle" "3.0.0" "$TEST_DIR" "gradle.properties"

  assert_success
  assert_output --partial "Bumping version to 3.0.0"
}

@test "bump-version.sh shows project type in output" {
  create_gradle_properties

  run_bump_version "gradle" "1.0.0" "$TEST_DIR" "gradle.properties"

  assert_success
  assert_output --partial "gradle"
}
