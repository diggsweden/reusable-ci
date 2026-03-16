#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/../test_helper.bash"

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

# Run validate-artifacts with debug output
run_validate_artifacts() {
  run_script "container/validate-artifacts.sh" "$@"
}

# =============================================================================
# Input Validation Tests
# =============================================================================

@test "validate-artifacts requires project-type argument" {
  run_validate_artifacts

  assert_failure
  assert_output --partial "Usage"
}

@test "validate-artifacts requires artifact-dir argument" {
  run_validate_artifacts "maven"

  assert_failure
  assert_output --partial "Usage"
}

@test "validate-artifacts fails for unknown project type" {
  mkdir -p artifacts

  run_validate_artifacts "unknown" "artifacts"

  assert_failure
  assert_output --partial "Unknown project type"
}

# =============================================================================
# Maven Artifact Tests
# =============================================================================

@test "validate-artifacts succeeds with Maven JAR artifacts" {
  mkdir -p target
  echo "jar content" > target/myapp.jar

  run_validate_artifacts "maven" "target"

  assert_success
  assert_output --partial "Maven artifacts found"
}

@test "validate-artifacts lists Maven JAR files" {
  mkdir -p target
  echo "jar1" > target/app.jar
  echo "jar2" > target/lib.jar

  run_validate_artifacts "maven" "target"

  assert_success
  assert_output --partial "app.jar"
  assert_output --partial "lib.jar"
}

@test "validate-artifacts warns when no Maven JARs found" {
  mkdir -p target
  # Empty directory

  run_validate_artifacts "maven" "target"

  assert_success  # Warning, not failure
  assert_output --partial "warning"
  assert_output --partial "No Maven artifacts"
}

@test "validate-artifacts warns when Maven artifact directory missing" {
  run_validate_artifacts "maven" "nonexistent"

  assert_success  # Warning, not failure
  assert_output --partial "warning"
}

# =============================================================================
# NPM Artifact Tests
# =============================================================================

@test "validate-artifacts succeeds with NPM artifacts" {
  mkdir -p dist
  echo "built js" > dist/index.js

  run_validate_artifacts "npm" "dist"

  assert_success
  assert_output --partial "NPM artifacts found"
}

@test "validate-artifacts lists NPM build files" {
  mkdir -p dist
  echo "js" > dist/main.js
  echo "css" > dist/styles.css

  run_validate_artifacts "npm" "dist"

  assert_success
  # Output shows files exist
}

@test "validate-artifacts warns when no NPM artifacts found" {
  mkdir -p dist
  # Empty directory

  run_validate_artifacts "npm" "dist"

  assert_success  # Warning, not failure
  assert_output --partial "warning"
  assert_output --partial "No NPM artifacts"
}

@test "validate-artifacts warns when NPM artifact directory missing" {
  run_validate_artifacts "npm" "nonexistent"

  assert_success  # Warning, not failure
  assert_output --partial "warning"
}

# =============================================================================
# Gradle Artifact Tests
# =============================================================================

@test "validate-artifacts succeeds with Gradle JAR artifacts" {
  mkdir -p build/libs
  echo "jar content" > build/libs/myapp.jar

  run_validate_artifacts "gradle" "build/libs"

  assert_success
  assert_output --partial "Gradle artifacts found"
}

@test "validate-artifacts lists Gradle JAR files" {
  mkdir -p build/libs
  echo "jar1" > build/libs/app.jar
  echo "jar2" > build/libs/app-sources.jar

  run_validate_artifacts "gradle" "build/libs"

  assert_success
  assert_output --partial "app.jar"
}

@test "validate-artifacts warns when no Gradle JARs found" {
  mkdir -p build/libs
  # Empty directory

  run_validate_artifacts "gradle" "build/libs"

  assert_success  # Warning, not failure
  assert_output --partial "warning"
  assert_output --partial "No Gradle artifacts"
}

@test "validate-artifacts warns when Gradle artifact directory missing" {
  run_validate_artifacts "gradle" "nonexistent"

  assert_success  # Warning, not failure
  assert_output --partial "warning"
}

# =============================================================================
# Containerfile Rebuild Detection Tests - Maven
# =============================================================================

@test "validate-artifacts warns when Containerfile rebuilds Maven project" {
  mkdir -p target
  echo "jar" > target/app.jar
  create_rebuild_containerfile "maven"

  run_validate_artifacts "maven" "target" "Containerfile"

  assert_success
  assert_output --partial "warning"
  assert_output --partial "rebuilds from source"
}

@test "validate-artifacts detects mvn package in Containerfile" {
  mkdir -p target
  echo "jar" > target/app.jar
  create_containerfile "FROM maven:3
RUN mvn clean package -DskipTests"

  run_validate_artifacts "maven" "target" "Containerfile"

  assert_success
  assert_output --partial "warning"
  assert_output --partial "rebuilds"
}

@test "validate-artifacts detects mvnw in Containerfile" {
  mkdir -p target
  echo "jar" > target/app.jar
  create_containerfile "FROM maven:3
RUN ./mvnw package"

  run_validate_artifacts "maven" "target" "Containerfile"

  assert_success
  assert_output --partial "warning"
}

# =============================================================================
# Containerfile Rebuild Detection Tests - NPM
# =============================================================================

@test "validate-artifacts warns when Containerfile rebuilds NPM project" {
  mkdir -p dist
  echo "js" > dist/index.js
  create_rebuild_containerfile "npm"

  run_validate_artifacts "npm" "dist" "Containerfile"

  assert_success
  assert_output --partial "warning"
  assert_output --partial "rebuilds from source"
}

@test "validate-artifacts detects npm run build in Containerfile" {
  mkdir -p dist
  echo "js" > dist/index.js
  create_containerfile "FROM node:18
RUN npm run build"

  run_validate_artifacts "npm" "dist" "Containerfile"

  assert_success
  assert_output --partial "warning"
}

# =============================================================================
# Containerfile Rebuild Detection Tests - Gradle
# =============================================================================

@test "validate-artifacts warns when Containerfile rebuilds Gradle project" {
  mkdir -p build/libs
  echo "jar" > build/libs/app.jar
  create_rebuild_containerfile "gradle"

  run_validate_artifacts "gradle" "build/libs" "Containerfile"

  assert_success
  assert_output --partial "warning"
  assert_output --partial "rebuilds from source"
}

@test "validate-artifacts detects gradle build in Containerfile" {
  mkdir -p build/libs
  echo "jar" > build/libs/app.jar
  create_containerfile "FROM gradle:8
RUN gradle assemble"

  run_validate_artifacts "gradle" "build/libs" "Containerfile"

  assert_success
  assert_output --partial "warning"
}

# =============================================================================
# No Warning Tests - Good Containerfiles
# =============================================================================

@test "validate-artifacts does not warn for artifact-only Containerfile" {
  mkdir -p target
  echo "jar" > target/app.jar
  create_artifact_containerfile

  run_validate_artifacts "maven" "target" "Containerfile"

  assert_success
  refute_output --partial "rebuilds from source"
}

@test "validate-artifacts does not warn when no Containerfile exists" {
  mkdir -p target
  echo "jar" > target/app.jar
  # No Containerfile

  run_validate_artifacts "maven" "target" "Containerfile"

  assert_success
  refute_output --partial "rebuilds"
}

@test "validate-artifacts does not warn for custom Containerfile path that doesn't exist" {
  mkdir -p target
  echo "jar" > target/app.jar

  run_validate_artifacts "maven" "target" "nonexistent/Containerfile"

  assert_success
  refute_output --partial "rebuilds"
}

# =============================================================================
# Warning Message Quality Tests
# =============================================================================

@test "validate-artifacts explains rebuild warning impact" {
  mkdir -p target
  echo "jar" > target/app.jar
  create_rebuild_containerfile "maven"

  run_validate_artifacts "maven" "target" "Containerfile"

  assert_success
  assert_output --partial "downloaded artifacts may be ignored"
}

@test "validate-artifacts suggests solution for rebuild warning" {
  mkdir -p target
  echo "jar" > target/app.jar
  create_rebuild_containerfile "maven"

  run_validate_artifacts "maven" "target" "Containerfile"

  assert_success
  assert_output --partial "COPY pre-built artifacts"
}

@test "validate-artifacts shows Containerfile path in warning" {
  mkdir -p target
  echo "jar" > target/app.jar
  create_rebuild_containerfile "maven"

  run_validate_artifacts "maven" "target" "Containerfile"

  assert_success
  assert_output --partial "Containerfile"
}

# =============================================================================
# Acceptable Missing Artifacts Message Tests
# =============================================================================

@test "validate-artifacts explains missing artifacts may be acceptable" {
  mkdir -p target
  # No JARs

  run_validate_artifacts "maven" "target"

  assert_success
  assert_output --partial "acceptable if container builds from source"
}

@test "validate-artifacts mentions container build may fail" {
  mkdir -p target
  # No JARs

  run_validate_artifacts "maven" "target"

  assert_success
  assert_output --partial "Container build may fail"
}

# =============================================================================
# GitHub Actions Annotation Tests
# =============================================================================

@test "validate-artifacts uses ::warning:: for missing artifacts" {
  mkdir -p target
  # Empty

  run_validate_artifacts "maven" "target"

  assert_success
  assert_output --partial "::warning::"
}

@test "validate-artifacts uses ::warning:: for rebuild detection" {
  mkdir -p target
  echo "jar" > target/app.jar
  create_rebuild_containerfile "maven"

  run_validate_artifacts "maven" "target" "Containerfile"

  assert_success
  assert_output --partial "::warning::"
}

# =============================================================================
# Edge Cases
# =============================================================================

@test "validate-artifacts handles artifact directory with spaces" {
  mkdir -p "my artifacts"
  echo "jar" > "my artifacts/app.jar"

  run_validate_artifacts "maven" "my artifacts"

  assert_success
}

@test "validate-artifacts handles Containerfile with complex patterns" {
  mkdir -p target
  echo "jar" > target/app.jar
  create_containerfile "FROM maven:3
# Comment about mvn
RUN apt-get update && \\
    mvn clean package && \\
    rm -rf /root/.m2"

  run_validate_artifacts "maven" "target" "Containerfile"

  assert_success
  assert_output --partial "warning"
}

@test "validate-artifacts handles Dockerfile as Containerfile path" {
  mkdir -p target
  echo "jar" > target/app.jar
  printf 'FROM maven:3\nRUN mvn package' > Dockerfile

  run_validate_artifacts "maven" "target" "Dockerfile"

  assert_success
  assert_output --partial "warning"
}
