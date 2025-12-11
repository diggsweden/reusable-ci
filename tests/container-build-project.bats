#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/test_helper.bash"

# =============================================================================
# Setup / Teardown
# =============================================================================

setup() {
  common_setup
}

teardown() {
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

# Run build-project with debug output
run_build_project() {
  run_script "container/build-project.sh" "$@"
}

# =============================================================================
# Input Validation Tests
# =============================================================================

@test "build-project.sh requires project type argument" {
  run_build_project

  assert_failure
}

@test "build-project.sh fails for unsupported project type" {
  run_build_project "unknown"

  assert_failure
  assert_output --partial "error"
  assert_output --partial "Unsupported project type"
}

@test "build-project.sh shows supported types on error" {
  run_build_project "invalid"

  assert_failure
  assert_output --partial "maven"
  assert_output --partial "npm"
  assert_output --partial "gradle"
}

# =============================================================================
# Maven Build Tests
# =============================================================================

@test "build-project.sh runs mvn clean package for maven type" {
  mock_mvn_success

  run_build_project "maven"

  assert_success
  assert_output --partial "Building Maven project"
}

@test "build-project.sh shows Maven completion message" {
  mock_mvn_success

  run_build_project "maven"

  assert_success
  assert_output --partial "Maven build completed"
}

@test "build-project.sh fails when mvn fails" {
  mock_mvn_failure "Compilation error"

  run_build_project "maven"

  assert_failure
}

@test "build-project.sh uses working directory for maven" {
  mkdir -p subdir
  mock_mvn_success
  
  run_build_project "maven" "subdir"

  assert_success
}

# =============================================================================
# NPM Build Tests
# =============================================================================

@test "build-project.sh runs npm ci and npm run build for npm type" {
  mock_npm_success

  run_build_project "npm"

  assert_success
  assert_output --partial "Building NPM project"
}

@test "build-project.sh shows NPM completion message" {
  mock_npm_success

  run_build_project "npm"

  assert_success
  assert_output --partial "NPM build completed"
}

@test "build-project.sh fails when npm ci fails" {
  mock_npm_failure "npm ERR! Could not resolve dependency"

  run_build_project "npm"

  assert_failure
}

@test "build-project.sh uses working directory for npm" {
  mkdir -p subdir
  mock_npm_success
  
  run_build_project "npm" "subdir"

  assert_success
}

# =============================================================================
# Gradle Build Tests
# =============================================================================

@test "build-project.sh runs gradlew clean build for gradle type" {
  mock_gradlew_success

  run_build_project "gradle"

  assert_success
  assert_output --partial "Building Gradle project"
}

@test "build-project.sh shows Gradle completion message" {
  mock_gradlew_success

  run_build_project "gradle"

  assert_success
  assert_output --partial "Gradle build completed"
}

@test "build-project.sh fails when gradlew fails" {
  mock_gradlew_failure "Build failed with exception"

  run_build_project "gradle"

  assert_failure
}

@test "build-project.sh uses working directory for gradle" {
  mkdir -p subdir
  # Create gradlew in the subdir where the build will run
  cat > "subdir/gradlew" << 'SCRIPT'
#!/usr/bin/env bash
printf "BUILD SUCCESSFUL\n"
SCRIPT
  chmod +x "subdir/gradlew"
  
  run_build_project "gradle" "subdir"

  assert_success
}

# =============================================================================
# Working Directory Tests
# =============================================================================

@test "build-project.sh defaults to current directory" {
  mock_mvn_success

  run_build_project "maven"

  assert_success
}

@test "build-project.sh fails for non-existent working directory" {
  mock_mvn_success

  run_build_project "maven" "nonexistent"

  assert_failure
}

@test "build-project.sh handles relative working directory" {
  mkdir -p my-project
  mock_mvn_success
  
  run_build_project "maven" "my-project"

  assert_success
}

@test "build-project.sh handles absolute working directory" {
  mkdir -p "${TEST_DIR}/abs-project"
  mock_mvn_success
  
  run_build_project "maven" "${TEST_DIR}/abs-project"

  assert_success
}

@test "build-project.sh handles working directory with spaces" {
  mkdir -p "my project"
  mock_mvn_success
  
  run_build_project "maven" "my project"

  assert_success
}

# =============================================================================
# Project Type Case Sensitivity Tests
# =============================================================================

@test "build-project.sh is case sensitive for maven" {
  mock_mvn_success

  run_build_project "Maven"

  assert_failure
  assert_output --partial "Unsupported project type"
}

@test "build-project.sh is case sensitive for npm" {
  mock_npm_success

  run_build_project "NPM"

  assert_failure
  assert_output --partial "Unsupported project type"
}

@test "build-project.sh is case sensitive for gradle" {
  mock_gradlew_success

  run_build_project "Gradle"

  assert_failure
  assert_output --partial "Unsupported project type"
}

# =============================================================================
# Error Annotation Tests
# =============================================================================

@test "build-project.sh uses GitHub Actions error annotation for unsupported type" {
  run_build_project "unknown"

  assert_failure
  assert_output --partial "::error::"
}

# =============================================================================
# Integration Tests with Real Project Structures
# =============================================================================

@test "build-project.sh maven works with pom.xml present" {
  create_maven_project
  mock_mvn_success

  run_build_project "maven"

  assert_success
}

@test "build-project.sh npm works with package.json present" {
  create_npm_project
  mock_npm_success

  run_build_project "npm"

  assert_success
}

@test "build-project.sh gradle works with build.gradle present" {
  create_gradle_project
  # Override the gradlew from create_gradle_project with success mock
  mock_gradlew_success

  run_build_project "gradle"

  assert_success
}
