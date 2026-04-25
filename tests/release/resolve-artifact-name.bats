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

run_resolve_artifact_name() {
  run_script "release/resolve-artifact-name.sh" "$@"
}

# =============================================================================
# Input Validation Tests
# =============================================================================

@test "resolve-artifact-name requires project-type argument" {
  run_resolve_artifact_name

  assert_failure
  assert_stderr_contains "Usage"
}

# =============================================================================
# Maven Project Tests
# =============================================================================

@test "resolve-artifact-name returns maven-build-artifacts and maven-build-sbom for maven" {
  run_resolve_artifact_name "maven"

  assert_success
  assert_line "name=maven-build-artifacts"
  assert_line "sbom-name=maven-build-sbom"
}

# =============================================================================
# NPM Project Tests
# =============================================================================

@test "resolve-artifact-name returns npm-build-artifacts and npm-build-sbom for npm" {
  run_resolve_artifact_name "npm"

  assert_success
  assert_line "name=npm-build-artifacts"
  assert_line "sbom-name=npm-build-sbom"
}

# =============================================================================
# Gradle Project Tests
# =============================================================================

@test "resolve-artifact-name returns default gradle names without ARTIFACT_NAME" {
  run_resolve_artifact_name "gradle"

  assert_success
  assert_line "name=gradle-build-artifacts"
  assert_line "sbom-name=gradle-build-sbom"
}

@test "resolve-artifact-name pairs gradle sbom with artifact-name override" {
  # Mirrors build-gradle-app.yml's pairing so release-create-github.yml can
  # locate the SBOM artifact uploaded by a matrix-named gradle build.
  ARTIFACT_NAME="my-service" run_resolve_artifact_name "gradle"

  assert_success
  assert_line "name=my-service"
  assert_line "sbom-name=my-service-sbom"
}

# =============================================================================
# Python Project Tests
# =============================================================================

@test "resolve-artifact-name returns python-build-artifacts and python-build-sbom for python" {
  run_resolve_artifact_name "python"

  assert_success
  assert_line "name=python-build-artifacts"
  assert_line "sbom-name=python-build-sbom"
}

# =============================================================================
# Go Project Tests
# =============================================================================

@test "resolve-artifact-name returns go-build-artifacts and go-build-sbom for go" {
  run_resolve_artifact_name "go"

  assert_success
  assert_line "name=go-build-artifacts"
  assert_line "sbom-name=go-build-sbom"
}

# =============================================================================
# Rust Project Tests
# =============================================================================

@test "resolve-artifact-name returns rust-build-sbom for rust" {
  run_resolve_artifact_name "rust"

  assert_success
  # Rust builder is SBOM-only, so build artifact and SBOM name coincide.
  assert_line "name=rust-build-sbom"
  assert_line "sbom-name=rust-build-sbom"
}

# =============================================================================
# Unknown Project Type Tests
# =============================================================================

@test "resolve-artifact-name returns generic names for unknown type" {
  run_resolve_artifact_name "unknown"

  assert_success
  assert_line "name=build-artifacts"
  assert_line "sbom-name=build-sbom"
}

@test "resolve-artifact-name handles empty-ish types as unknown" {
  run_resolve_artifact_name "other"

  assert_success
  assert_line "name=build-artifacts"
  assert_line "sbom-name=build-sbom"
}

# =============================================================================
# Output Format Tests
# =============================================================================

@test "resolve-artifact-name output is in name=value format" {
  run_resolve_artifact_name "maven"

  assert_success
  assert_line --regexp "^name=.+$"
  assert_line --regexp "^sbom-name=.+$"
}

@test "resolve-artifact-name outputs exactly two lines" {
  run_resolve_artifact_name "maven"

  assert_success
  local line_count
  line_count=$(printf '%s\n' "$output" | wc -l)
  [[ "$line_count" -eq 2 ]]
}

# =============================================================================
# Case Sensitivity Tests
# =============================================================================

@test "resolve-artifact-name is case sensitive - Maven not recognized" {
  run_resolve_artifact_name "Maven"

  assert_success
  assert_line "name=build-artifacts"
  assert_line "sbom-name=build-sbom"
}

@test "resolve-artifact-name is case sensitive - NPM not recognized" {
  run_resolve_artifact_name "NPM"

  assert_success
  assert_line "name=build-artifacts"
  assert_line "sbom-name=build-sbom"
}
