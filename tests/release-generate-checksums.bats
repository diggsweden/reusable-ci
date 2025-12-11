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

# Run generate-checksums with debug output
run_generate_checksums() {
  run_script "release/generate-checksums.sh" "$@"
}

# Create a test artifact file
create_artifact() {
  local dir="$1"
  local filename="$2"
  local content="${3:-test content}"
  
  mkdir -p "$dir"
  echo "$content" > "$dir/$filename"
}

# Get SHA256 of a file (portable)
get_sha256() {
  sha256sum "$1" | cut -d' ' -f1
}

# =============================================================================
# Basic Functionality Tests
# =============================================================================

@test "generate-checksums.sh creates checksum file" {
  mkdir -p release-artifacts
  echo "test" > release-artifacts/test.jar
  
  run_generate_checksums "checksums.sha256" "./release-artifacts"

  assert_success
  assert_file_exists "checksums.sha256"
}

@test "generate-checksums.sh uses default output filename" {
  mkdir -p release-artifacts
  echo "test" > release-artifacts/test.jar
  
  run_generate_checksums

  assert_success
  assert_file_exists "checksums.sha256"
}

@test "generate-checksums.sh reports checksum count" {
  mkdir -p release-artifacts
  echo "file1" > release-artifacts/file1.jar
  echo "file2" > release-artifacts/file2.jar
  
  run_generate_checksums "checksums.sha256" "./release-artifacts"

  assert_success
  assert_output --partial "Generated"
  assert_output --partial "checksums"
}

# =============================================================================
# Release Artifacts Tests
# =============================================================================

@test "generate-checksums.sh checksums release artifacts" {
  create_artifact "release-artifacts" "app.jar" "application content"
  
  run_generate_checksums "checksums.sha256" "./release-artifacts"

  assert_success
  
  # Verify checksum file contains the artifact
  run cat checksums.sha256
  assert_output --partial "app.jar"
}

@test "generate-checksums.sh includes correct SHA256 hash" {
  create_artifact "release-artifacts" "test.txt" "known content"
  local expected_hash
  expected_hash=$(get_sha256 "release-artifacts/test.txt")
  
  run_generate_checksums "checksums.sha256" "./release-artifacts"

  assert_success
  run cat checksums.sha256
  assert_output --partial "$expected_hash"
}

@test "generate-checksums.sh handles multiple release artifacts" {
  create_artifact "release-artifacts" "app.jar" "app"
  create_artifact "release-artifacts" "lib.jar" "lib"
  create_artifact "release-artifacts" "docs.zip" "docs"
  
  run_generate_checksums "checksums.sha256" "./release-artifacts"

  assert_success
  run cat checksums.sha256
  assert_output --partial "app.jar"
  assert_output --partial "lib.jar"
  assert_output --partial "docs.zip"
}

@test "generate-checksums.sh strips directory prefix from artifact paths" {
  create_artifact "release-artifacts" "myapp.jar" "content"
  
  run_generate_checksums "checksums.sha256" "./release-artifacts"

  assert_success
  run cat checksums.sha256
  # Should be "myapp.jar" not "./release-artifacts/myapp.jar"
  refute_output --partial "release-artifacts/"
  assert_output --partial "myapp.jar"
}

# =============================================================================
# Attached Patterns Tests
# =============================================================================

@test "generate-checksums.sh processes attach patterns" {
  echo "attachment" > attached-file.txt
  
  run_generate_checksums "checksums.sha256" "./release-artifacts" "attached-file.txt"

  assert_success
  run cat checksums.sha256
  assert_output --partial "attached-file.txt"
}

@test "generate-checksums.sh handles comma-separated patterns" {
  echo "file1" > file1.txt
  echo "file2" > file2.md
  
  run_generate_checksums "checksums.sha256" "./release-artifacts" "file1.txt,file2.md"

  assert_success
  run cat checksums.sha256
  assert_output --partial "file1.txt"
  assert_output --partial "file2.md"
}

@test "generate-checksums.sh handles glob patterns" {
  echo "a" > test-a.txt
  echo "b" > test-b.txt
  
  run_generate_checksums "checksums.sha256" "./release-artifacts" "test-*.txt"

  assert_success
  run cat checksums.sha256
  assert_output --partial "test-a.txt"
  assert_output --partial "test-b.txt"
}

# =============================================================================
# SBOM Directory Tests
# =============================================================================

@test "generate-checksums.sh processes SBOM container files" {
  mkdir -p sbom-artifacts
  echo "sbom" > "sbom-artifacts/myapp-container-sbom.spdx.json"
  
  run_generate_checksums "checksums.sha256" "./release-artifacts" "" "./sbom-artifacts"

  assert_success
  run cat checksums.sha256
  assert_output --partial "myapp-container-sbom.spdx.json"
}

@test "generate-checksums.sh handles cyclonedx container SBOMs" {
  mkdir -p sbom-artifacts
  echo "cdx" > "sbom-artifacts/app-container-sbom.cyclonedx.json"
  
  run_generate_checksums "checksums.sha256" "./release-artifacts" "" "./sbom-artifacts"

  assert_success
  run cat checksums.sha256
  assert_output --partial "app-container-sbom.cyclonedx.json"
}

# =============================================================================
# SBOM Layer Files Tests
# =============================================================================

@test "generate-checksums.sh includes pom SBOM files" {
  echo "pom sbom" > "myapp-pom-sbom.spdx.json"
  
  run_generate_checksums "checksums.sha256" "./release-artifacts" "" "./sbom-artifacts"

  assert_success
  run cat checksums.sha256
  assert_output --partial "myapp-pom-sbom.spdx.json"
}

@test "generate-checksums.sh includes package SBOM files" {
  echo "package sbom" > "myapp-package-sbom.cyclonedx.json"
  
  run_generate_checksums "checksums.sha256" "./release-artifacts" "" "./sbom-artifacts"

  assert_success
  run cat checksums.sha256
  assert_output --partial "myapp-package-sbom.cyclonedx.json"
}

@test "generate-checksums.sh includes jar SBOM files" {
  echo "jar sbom" > "mylib-jar-sbom.spdx.json"
  
  run_generate_checksums "checksums.sha256" "./release-artifacts" "" "./sbom-artifacts"

  assert_success
  run cat checksums.sha256
  assert_output --partial "mylib-jar-sbom.spdx.json"
}

@test "generate-checksums.sh includes gradle SBOM files" {
  echo "gradle sbom" > "app-gradle-sbom.cyclonedx.json"
  
  run_generate_checksums "checksums.sha256" "./release-artifacts" "" "./sbom-artifacts"

  assert_success
  run cat checksums.sha256
  assert_output --partial "app-gradle-sbom.cyclonedx.json"
}

# =============================================================================
# Empty/Missing Directory Tests
# =============================================================================

@test "generate-checksums.sh handles missing release-artifacts directory" {
  run_generate_checksums "checksums.sha256" "./nonexistent"

  assert_success
  assert_file_exists "checksums.sha256"
}

@test "generate-checksums.sh handles empty release-artifacts directory" {
  mkdir -p release-artifacts
  
  run_generate_checksums "checksums.sha256" "./release-artifacts"

  assert_success
  assert_output --partial "Generated 0 checksums"
}

@test "generate-checksums.sh handles missing SBOM directory" {
  mkdir -p release-artifacts
  echo "app" > release-artifacts/app.jar
  
  run_generate_checksums "checksums.sha256" "./release-artifacts" "" "./missing-sbom"

  assert_success
}

# =============================================================================
# Output Format Tests
# =============================================================================

@test "generate-checksums.sh creates SHA256 format output" {
  create_artifact "release-artifacts" "test.jar" "content"
  
  run_generate_checksums "checksums.sha256" "./release-artifacts"

  assert_success
  
  # SHA256 should be 64 hex characters
  run cat checksums.sha256
  assert_output --regexp "^[a-f0-9]{64}[[:space:]]"
}

@test "generate-checksums.sh output is verifiable with sha256sum" {
  create_artifact "release-artifacts" "verifiable.txt" "verify this"
  
  run_generate_checksums "checksums.sha256" "./release-artifacts"

  assert_success
  
  # Verify the checksums are correct
  cd release-artifacts
  run sha256sum -c ../checksums.sha256
  assert_success
}

# =============================================================================
# Custom Output File Tests
# =============================================================================

@test "generate-checksums.sh uses custom output filename" {
  mkdir -p release-artifacts
  echo "test" > release-artifacts/test.jar
  
  run_generate_checksums "custom-checksums.txt" "./release-artifacts"

  assert_success
  assert_file_exists "custom-checksums.txt"
  assert_file_not_exists "checksums.sha256"
}

@test "generate-checksums.sh creates output in specified path" {
  mkdir -p release-artifacts output
  echo "test" > release-artifacts/test.jar
  
  run_generate_checksums "output/checksums.sha256" "./release-artifacts"

  assert_success
  assert_file_exists "output/checksums.sha256"
}
