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
  common_setup_with_git
}

teardown() {
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

run_create_sbom_zip() {
  run_script "release/create-sbom-zip.sh" "$@"
}

# Create various SBOM files for testing
create_pom_sbom() {
  local project="${1:-myapp}"
  printf '{"spdx": "pom sbom"}' > "${project}-pom-sbom.spdx.json"
}

create_package_sbom() {
  local project="${1:-myapp}"
  printf '{"spdx": "package sbom"}' > "${project}-package-sbom.spdx.json"
}

create_jar_sbom() {
  local project="${1:-myapp}"
  printf '{"spdx": "jar sbom"}' > "${project}-jar-sbom.spdx.json"
}

create_gradle_sbom() {
  local project="${1:-myapp}"
  printf '{"cyclonedx": "gradle sbom"}' > "${project}-gradle-sbom.cyclonedx.json"
}

create_container_sbom() {
  local project="${1:-myapp}"
  mkdir -p sbom-artifacts
  printf '{"spdx": "container sbom"}' > "sbom-artifacts/${project}-container-sbom.spdx.json"
}

# =============================================================================
# No SBOM Tests
# =============================================================================

@test "create-sbom-zip.sh skips when no SBOMs found" {
  run_create_sbom_zip "myapp" "1.0.0"

  assert_success
  assert_output --partial "No SBOMs found"
  assert_output --partial "skipping"
}

@test "create-sbom-zip.sh does not create zip when no SBOMs" {
  run_create_sbom_zip "myapp" "1.0.0"

  assert_success
  assert_file_not_exists "myapp-1.0.0-sboms.zip"
}

# =============================================================================
# Basic ZIP Creation Tests
# =============================================================================

@test "create-sbom-zip.sh creates zip with pom SBOM" {
  create_pom_sbom "myapp"

  run_create_sbom_zip "myapp" "1.0.0"

  assert_success
  assert_file_exists "myapp-1.0.0-sboms.zip"
}

@test "create-sbom-zip.sh creates zip with package SBOM" {
  create_package_sbom "myapp"

  run_create_sbom_zip "myapp" "1.0.0"

  assert_success
  assert_file_exists "myapp-1.0.0-sboms.zip"
}

@test "create-sbom-zip.sh creates zip with jar SBOM" {
  create_jar_sbom "myapp"

  run_create_sbom_zip "myapp" "1.0.0"

  assert_success
  assert_file_exists "myapp-1.0.0-sboms.zip"
}

@test "create-sbom-zip.sh creates zip with gradle SBOM" {
  create_gradle_sbom "myapp"

  run_create_sbom_zip "myapp" "1.0.0"

  assert_success
  assert_file_exists "myapp-1.0.0-sboms.zip"
}

# =============================================================================
# Container SBOM Tests
# =============================================================================

@test "create-sbom-zip.sh includes container SBOMs" {
  create_container_sbom "myapp"

  run_create_sbom_zip "myapp" "1.0.0"

  assert_success
  assert_file_exists "myapp-1.0.0-sboms.zip"
  assert_output --partial "container-sbom"
}

@test "create-sbom-zip.sh handles container SBOMs with -j flag" {
  create_container_sbom "myapp"

  run_create_sbom_zip "myapp" "1.0.0"

  assert_success
  # Verify file is in zip without directory path
  run unzip -l myapp-1.0.0-sboms.zip
  assert_output --partial "myapp-container-sbom.spdx.json"
  refute_output --partial "sbom-artifacts/"
}

# =============================================================================
# Multiple SBOM Layers Tests
# =============================================================================

@test "create-sbom-zip.sh includes all three layers" {
  create_pom_sbom "myapp"
  create_jar_sbom "myapp"
  create_container_sbom "myapp"

  run_create_sbom_zip "myapp" "1.0.0"

  assert_success
  
  run unzip -l myapp-1.0.0-sboms.zip
  assert_output --partial "pom-sbom"
  assert_output --partial "jar-sbom"
  assert_output --partial "container-sbom"
}

@test "create-sbom-zip.sh shows added files" {
  create_pom_sbom "myapp"
  create_jar_sbom "myapp"

  run_create_sbom_zip "myapp" "1.0.0"

  assert_success
  assert_output --partial "Added:"
  assert_output --partial "pom-sbom"
  assert_output --partial "jar-sbom"
}

# =============================================================================
# Version Handling Tests
# =============================================================================

@test "create-sbom-zip.sh strips v prefix from version" {
  create_pom_sbom "myapp"

  run_create_sbom_zip "myapp" "v1.0.0"

  assert_success
  assert_file_exists "myapp-1.0.0-sboms.zip"
  assert_file_not_exists "myapp-v1.0.0-sboms.zip"
}

@test "create-sbom-zip.sh handles version without v prefix" {
  create_pom_sbom "myapp"

  run_create_sbom_zip "myapp" "2.0.0"

  assert_success
  assert_file_exists "myapp-2.0.0-sboms.zip"
}

@test "create-sbom-zip.sh handles prerelease versions" {
  create_pom_sbom "myapp"

  run_create_sbom_zip "myapp" "1.0.0-SNAPSHOT"

  assert_success
  assert_file_exists "myapp-1.0.0-SNAPSHOT-sboms.zip"
}

# =============================================================================
# Project Name Tests
# =============================================================================

@test "create-sbom-zip.sh uses provided project name" {
  create_pom_sbom "custom-project"

  run_create_sbom_zip "custom-project" "1.0.0"

  assert_success
  assert_file_exists "custom-project-1.0.0-sboms.zip"
}

@test "create-sbom-zip.sh handles project name with hyphens" {
  create_pom_sbom "my-cool-project"

  run_create_sbom_zip "my-cool-project" "1.0.0"

  assert_success
  assert_file_exists "my-cool-project-1.0.0-sboms.zip"
}

# =============================================================================
# ZIP Contents Verification Tests
# =============================================================================

@test "create-sbom-zip.sh shows zip contents" {
  create_pom_sbom "myapp"

  run_create_sbom_zip "myapp" "1.0.0"

  assert_success
  assert_output --partial "SBOM ZIP contents"
}

@test "create-sbom-zip.sh shows completion message" {
  create_pom_sbom "myapp"

  run_create_sbom_zip "myapp" "1.0.0"

  assert_success
  assert_output --partial "Created SBOM ZIP"
}

# =============================================================================
# SPDX vs CycloneDX Format Tests
# =============================================================================

@test "create-sbom-zip.sh includes SPDX format SBOMs" {
  printf '{"spdxVersion": "2.3"}' > "myapp-pom-sbom.spdx.json"

  run_create_sbom_zip "myapp" "1.0.0"

  assert_success
  run unzip -l myapp-1.0.0-sboms.zip
  assert_output --partial ".spdx.json"
}

@test "create-sbom-zip.sh includes CycloneDX format SBOMs" {
  printf '{"bomFormat": "CycloneDX"}' > "myapp-pom-sbom.cyclonedx.json"

  run_create_sbom_zip "myapp" "1.0.0"

  assert_success
  run unzip -l myapp-1.0.0-sboms.zip
  assert_output --partial ".cyclonedx.json"
}

@test "create-sbom-zip.sh handles mixed SBOM formats" {
  printf '{"spdxVersion": "2.3"}' > "myapp-pom-sbom.spdx.json"
  printf '{"bomFormat": "CycloneDX"}' > "myapp-jar-sbom.cyclonedx.json"

  run_create_sbom_zip "myapp" "1.0.0"

  assert_success
  run unzip -l myapp-1.0.0-sboms.zip
  assert_output --partial ".spdx.json"
  assert_output --partial ".cyclonedx.json"
}

# =============================================================================
# Edge Cases
# =============================================================================

@test "create-sbom-zip.sh handles empty sbom-artifacts directory" {
  mkdir -p sbom-artifacts
  create_pom_sbom "myapp"

  run_create_sbom_zip "myapp" "1.0.0"

  assert_success
  assert_file_exists "myapp-1.0.0-sboms.zip"
}

@test "create-sbom-zip.sh counts SBOMs correctly" {
  create_pom_sbom "myapp"
  create_jar_sbom "myapp"
  create_container_sbom "myapp"

  run_create_sbom_zip "myapp" "1.0.0"

  assert_success
  # Should find 3 SBOMs and create zip
  assert_file_exists "myapp-1.0.0-sboms.zip"
}

# =============================================================================
# Tararchive SBOM Tests (NPM)
# =============================================================================

@test "create-sbom-zip.sh includes tararchive SBOMs" {
  printf '{"spdx": "tararchive"}' > "myapp-tararchive-sbom.spdx.json"

  run_create_sbom_zip "myapp" "1.0.0"

  assert_success
  run unzip -l myapp-1.0.0-sboms.zip
  assert_output --partial "tararchive-sbom"
}
