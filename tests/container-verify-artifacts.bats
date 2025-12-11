#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
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

# Run verify-artifacts with debug output
run_verify_artifacts() {
  run_script "container/verify-artifacts.sh" "$@"
}

# =============================================================================
# Input Validation Tests
# =============================================================================

@test "verify-artifacts.sh requires project-type argument" {
  run_verify_artifacts

  assert_failure
  assert_output --partial "Usage"
}

@test "verify-artifacts.sh requires artifact-dir argument" {
  run_verify_artifacts "maven"

  assert_failure
  assert_output --partial "Usage"
}

@test "verify-artifacts.sh fails for unknown project type" {
  mkdir -p artifacts

  run_verify_artifacts "unknown" "artifacts"

  assert_failure
  assert_output --partial "Unknown project type"
}

# =============================================================================
# Maven Artifact Tests
# =============================================================================

@test "verify-artifacts.sh succeeds with Maven JAR artifacts" {
  mkdir -p target
  echo "jar content" > target/myapp.jar

  run_verify_artifacts "maven" "target"

  assert_success
  assert_output --partial "Maven artifacts found"
}

@test "verify-artifacts.sh lists Maven JAR files" {
  mkdir -p target
  echo "jar1" > target/app.jar
  echo "jar2" > target/lib.jar

  run_verify_artifacts "maven" "target"

  assert_success
  assert_output --partial "app.jar"
  assert_output --partial "lib.jar"
}

@test "verify-artifacts.sh warns when no Maven JARs found" {
  mkdir -p target
  # Empty directory

  run_verify_artifacts "maven" "target"

  assert_success  # Warning, not failure
  assert_output --partial "warning"
  assert_output --partial "No Maven artifacts"
}

@test "verify-artifacts.sh warns when Maven artifact directory missing" {
  run_verify_artifacts "maven" "nonexistent"

  assert_success  # Warning, not failure
  assert_output --partial "warning"
}

# =============================================================================
# NPM Artifact Tests
# =============================================================================

@test "verify-artifacts.sh succeeds with NPM artifacts" {
  mkdir -p dist
  echo "built js" > dist/index.js

  run_verify_artifacts "npm" "dist"

  assert_success
  assert_output --partial "NPM artifacts found"
}

@test "verify-artifacts.sh lists NPM build files" {
  mkdir -p dist
  echo "js" > dist/main.js
  echo "css" > dist/styles.css

  run_verify_artifacts "npm" "dist"

  assert_success
  # Output shows files exist
}

@test "verify-artifacts.sh warns when no NPM artifacts found" {
  mkdir -p dist
  # Empty directory

  run_verify_artifacts "npm" "dist"

  assert_success  # Warning, not failure
  assert_output --partial "warning"
  assert_output --partial "No NPM artifacts"
}

@test "verify-artifacts.sh warns when NPM artifact directory missing" {
  run_verify_artifacts "npm" "nonexistent"

  assert_success  # Warning, not failure
  assert_output --partial "warning"
}

# =============================================================================
# Gradle Artifact Tests
# =============================================================================

@test "verify-artifacts.sh succeeds with Gradle JAR artifacts" {
  mkdir -p build/libs
  echo "jar content" > build/libs/myapp.jar

  run_verify_artifacts "gradle" "build/libs"

  assert_success
  assert_output --partial "Gradle artifacts found"
}

@test "verify-artifacts.sh lists Gradle JAR files" {
  mkdir -p build/libs
  echo "jar1" > build/libs/app.jar
  echo "jar2" > build/libs/app-sources.jar

  run_verify_artifacts "gradle" "build/libs"

  assert_success
  assert_output --partial "app.jar"
}

@test "verify-artifacts.sh warns when no Gradle JARs found" {
  mkdir -p build/libs
  # Empty directory

  run_verify_artifacts "gradle" "build/libs"

  assert_success  # Warning, not failure
  assert_output --partial "warning"
  assert_output --partial "No Gradle artifacts"
}

@test "verify-artifacts.sh warns when Gradle artifact directory missing" {
  run_verify_artifacts "gradle" "nonexistent"

  assert_success  # Warning, not failure
  assert_output --partial "warning"
}

# =============================================================================
# Containerfile Rebuild Detection Tests - Maven
# =============================================================================

@test "verify-artifacts.sh warns when Containerfile rebuilds Maven project" {
  mkdir -p target
  echo "jar" > target/app.jar
  create_rebuild_containerfile "maven"

  run_verify_artifacts "maven" "target" "Containerfile"

  assert_success
  assert_output --partial "warning"
  assert_output --partial "rebuilds from source"
}

@test "verify-artifacts.sh detects mvn package in Containerfile" {
  mkdir -p target
  echo "jar" > target/app.jar
  create_containerfile "FROM maven:3
RUN mvn clean package -DskipTests"

  run_verify_artifacts "maven" "target" "Containerfile"

  assert_success
  assert_output --partial "warning"
  assert_output --partial "rebuilds"
}

@test "verify-artifacts.sh detects mvnw in Containerfile" {
  mkdir -p target
  echo "jar" > target/app.jar
  create_containerfile "FROM maven:3
RUN ./mvnw package"

  run_verify_artifacts "maven" "target" "Containerfile"

  assert_success
  assert_output --partial "warning"
}

# =============================================================================
# Containerfile Rebuild Detection Tests - NPM
# =============================================================================

@test "verify-artifacts.sh warns when Containerfile rebuilds NPM project" {
  mkdir -p dist
  echo "js" > dist/index.js
  create_rebuild_containerfile "npm"

  run_verify_artifacts "npm" "dist" "Containerfile"

  assert_success
  assert_output --partial "warning"
  assert_output --partial "rebuilds from source"
}

@test "verify-artifacts.sh detects npm run build in Containerfile" {
  mkdir -p dist
  echo "js" > dist/index.js
  create_containerfile "FROM node:18
RUN npm run build"

  run_verify_artifacts "npm" "dist" "Containerfile"

  assert_success
  assert_output --partial "warning"
}

# =============================================================================
# Containerfile Rebuild Detection Tests - Gradle
# =============================================================================

@test "verify-artifacts.sh warns when Containerfile rebuilds Gradle project" {
  mkdir -p build/libs
  echo "jar" > build/libs/app.jar
  create_rebuild_containerfile "gradle"

  run_verify_artifacts "gradle" "build/libs" "Containerfile"

  assert_success
  assert_output --partial "warning"
  assert_output --partial "rebuilds from source"
}

@test "verify-artifacts.sh detects gradle build in Containerfile" {
  mkdir -p build/libs
  echo "jar" > build/libs/app.jar
  create_containerfile "FROM gradle:8
RUN gradle assemble"

  run_verify_artifacts "gradle" "build/libs" "Containerfile"

  assert_success
  assert_output --partial "warning"
}

# =============================================================================
# No Warning Tests - Good Containerfiles
# =============================================================================

@test "verify-artifacts.sh does not warn for artifact-only Containerfile" {
  mkdir -p target
  echo "jar" > target/app.jar
  create_artifact_containerfile

  run_verify_artifacts "maven" "target" "Containerfile"

  assert_success
  refute_output --partial "rebuilds from source"
}

@test "verify-artifacts.sh does not warn when no Containerfile exists" {
  mkdir -p target
  echo "jar" > target/app.jar
  # No Containerfile

  run_verify_artifacts "maven" "target" "Containerfile"

  assert_success
  refute_output --partial "rebuilds"
}

@test "verify-artifacts.sh does not warn for custom Containerfile path that doesn't exist" {
  mkdir -p target
  echo "jar" > target/app.jar

  run_verify_artifacts "maven" "target" "nonexistent/Containerfile"

  assert_success
  refute_output --partial "rebuilds"
}

# =============================================================================
# Warning Message Quality Tests
# =============================================================================

@test "verify-artifacts.sh explains rebuild warning impact" {
  mkdir -p target
  echo "jar" > target/app.jar
  create_rebuild_containerfile "maven"

  run_verify_artifacts "maven" "target" "Containerfile"

  assert_success
  assert_output --partial "downloaded artifacts may be ignored"
}

@test "verify-artifacts.sh suggests solution for rebuild warning" {
  mkdir -p target
  echo "jar" > target/app.jar
  create_rebuild_containerfile "maven"

  run_verify_artifacts "maven" "target" "Containerfile"

  assert_success
  assert_output --partial "COPY pre-built artifacts"
}

@test "verify-artifacts.sh shows Containerfile path in warning" {
  mkdir -p target
  echo "jar" > target/app.jar
  create_rebuild_containerfile "maven"

  run_verify_artifacts "maven" "target" "Containerfile"

  assert_success
  assert_output --partial "Containerfile"
}

# =============================================================================
# Acceptable Missing Artifacts Message Tests
# =============================================================================

@test "verify-artifacts.sh explains missing artifacts may be acceptable" {
  mkdir -p target
  # No JARs

  run_verify_artifacts "maven" "target"

  assert_success
  assert_output --partial "acceptable if container builds from source"
}

@test "verify-artifacts.sh mentions container build may fail" {
  mkdir -p target
  # No JARs

  run_verify_artifacts "maven" "target"

  assert_success
  assert_output --partial "Container build may fail"
}

# =============================================================================
# GitHub Actions Annotation Tests
# =============================================================================

@test "verify-artifacts.sh uses ::warning:: for missing artifacts" {
  mkdir -p target
  # Empty

  run_verify_artifacts "maven" "target"

  assert_success
  assert_output --partial "::warning::"
}

@test "verify-artifacts.sh uses ::warning:: for rebuild detection" {
  mkdir -p target
  echo "jar" > target/app.jar
  create_rebuild_containerfile "maven"

  run_verify_artifacts "maven" "target" "Containerfile"

  assert_success
  assert_output --partial "::warning::"
}

# =============================================================================
# Edge Cases
# =============================================================================

@test "verify-artifacts.sh handles artifact directory with spaces" {
  mkdir -p "my artifacts"
  echo "jar" > "my artifacts/app.jar"

  run_verify_artifacts "maven" "my artifacts"

  assert_success
}

@test "verify-artifacts.sh handles Containerfile with complex patterns" {
  mkdir -p target
  echo "jar" > target/app.jar
  create_containerfile "FROM maven:3
# Comment about mvn
RUN apt-get update && \\
    mvn clean package && \\
    rm -rf /root/.m2"

  run_verify_artifacts "maven" "target" "Containerfile"

  assert_success
  assert_output --partial "warning"
}

@test "verify-artifacts.sh handles Dockerfile as Containerfile path" {
  mkdir -p target
  echo "jar" > target/app.jar
  printf 'FROM maven:3\nRUN mvn package' > Dockerfile

  run_verify_artifacts "maven" "target" "Dockerfile"

  assert_success
  assert_output --partial "warning"
}
