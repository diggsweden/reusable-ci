#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2103,SC2119,SC2120,SC2155
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
  create_mock_syft
}

teardown() {
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

run_generate_sbom() {
  run_script "sbom/generate-sbom.sh" "$@"
}

# =============================================================================
# Project Detection Tests
# =============================================================================

@test "generate-sbomdetects maven project from pom.xml" {
  create_maven_project

  run_generate_sbom "auto" "source" "" "" "."

  assert_success
  assert_output --partial "Auto-detected project type: maven"
}

@test "generate-sbomdetects npm project from package.json" {
  create_npm_project

  run_generate_sbom "auto" "source" "" "" "."

  assert_success
  assert_output --partial "Auto-detected project type: npm"
}

@test "generate-sbomdetects gradle project from build.gradle" {
  create_gradle_project

  run_generate_sbom "auto" "source" "" "" "."

  assert_success
  assert_output --partial "Auto-detected project type: gradle"
}

@test "generate-sbomdetects go project from go.mod" {
  cat > go.mod << 'EOF'
module github.com/example/myapp

go 1.21
EOF

  run_generate_sbom "auto" "source" "" "" "."

  assert_success
  assert_output --partial "Auto-detected project type: go"
}

@test "generate-sbomdetects rust project from Cargo.toml" {
  cat > Cargo.toml << 'EOF'
[package]
name = "myapp"
version = "1.0.0"
EOF

  run_generate_sbom "auto" "source" "" "" "."

  assert_success
  assert_output --partial "Auto-detected project type: rust"
}

@test "generate-sbomdetects python project from pyproject.toml" {
  cat > pyproject.toml << 'EOF'
[project]
name = "myapp"
version = "1.0.0"
EOF

  run_generate_sbom "auto" "source" "" "" "."

  assert_success
  assert_output --partial "Auto-detected project type: python"
}

@test "generate-sbomdetects python project from requirements.txt" {
  echo "requests==2.28.0" > requirements.txt

  run_generate_sbom "auto" "source" "" "" "."

  assert_success
  assert_output --partial "Auto-detected project type: python"
}

@test "generate-sbomuses explicit project type over auto-detect" {
  # Create both maven and npm project files
  create_maven_project
  create_npm_project

  run_generate_sbom "npm" "source" "" "" "."

  assert_success
  assert_output --partial "Project type: npm"
  refute_output --partial "Auto-detected"
}

# =============================================================================
# Source Layer Tests
# =============================================================================

@test "generate-sbomgenerates source layer for maven" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Generating Source layer"
  assert_output --partial "Scanning pom.xml"
}

@test "generate-sbomgenerates source layer for npm" {
  create_npm_project

  run_generate_sbom "npm" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Generating Source layer"
  assert_output --partial "Scanning package.json"
}

@test "generate-sbomgenerates source layer for gradle" {
  create_gradle_project

  run_generate_sbom "gradle" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Generating Source layer"
  assert_output --partial "Scanning build.gradle"
}

@test "generate-sbomwarns when source manifest missing" {
  # Empty directory, no pom.xml - script fails when no SBOMs generated

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "."

  assert_failure  # Exits 1 when no SBOMs generated
  assert_output --partial "No pom.xml found"
  assert_output --partial "No SBOM files generated"
}

# =============================================================================
# Build Layer Tests
# =============================================================================

@test "generate-sbomgenerates build layer for maven when bom.json exists" {
  create_maven_project false
  mkdir -p target
  echo '{"bomFormat":"CycloneDX"}' > target/bom.json

  run_generate_sbom "maven" "build" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Generating Build layer"
}

@test "generate-sbomwarns when no bom.json found for maven build layer" {
  create_maven_project false

  run_generate_sbom "maven" "build" "1.0.0" "myapp" "."

  assert_failure
  assert_output --partial "No Maven Build SBOM"
}

@test "generate-sbombuild layer included in multi-layer run does not break packaging" {
  create_maven_project true
  mkdir -p target
  echo '{"bomFormat":"CycloneDX"}' > target/bom.json

  run_generate_sbom "maven" "source,build,analyzed-artifact" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Generating Source layer"
  assert_output --partial "Generating Build layer"
  assert_output --partial "Generating Artifact layer"
}

# =============================================================================
# Artifact Layer Tests
# =============================================================================

@test "generate-sbomgenerates artifact layer for maven JAR" {
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  run_generate_sbom "maven" "analyzed-artifact" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Generating Artifact layer"
}

@test "generate-sbomgenerates artifact layer for maven JAR without version suffix (finalName)" {
  create_maven_project false
  mkdir -p target
  echo "mock jar" > target/myapp.jar

  run_generate_sbom "maven" "analyzed-artifact" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Generating Artifact layer"
}

@test "generate-sbomwarns when no JARs found for maven" {
  create_maven_project false  # No JAR - script fails when no SBOMs generated

  run_generate_sbom "maven" "analyzed-artifact" "1.0.0" "myapp" "."

  assert_failure  # Exits 1 when no SBOMs generated
  assert_output --partial "No JAR files found"
  assert_output --partial "No SBOM files generated"
}

@test "generate-sbomgenerates artifact layer for npm tarball" {
  create_npm_project
  echo "mock tarball" > myapp-1.0.0.tgz

  run_generate_sbom "npm" "analyzed-artifact" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Generating Artifact layer"
}

@test "generate-sbomgenerates artifact layer for gradle" {
  create_gradle_project true

  run_generate_sbom "gradle" "analyzed-artifact" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Generating Artifact layer"
}

# =============================================================================
# Container Layer Tests
# =============================================================================

@test "generate-sbomgenerates container layer when image provided" {
  run_generate_sbom "maven" "analyzed-container" "1.0.0" "myapp" "." "ghcr.io/org/myapp:1.0.0"

  assert_success
  assert_output --partial "Generating Container layer"
  assert_output --partial "ghcr.io/org/myapp:1.0.0"
}

@test "generate-sbomskips container layer when no image provided" {
  run_generate_sbom "maven" "analyzed-container" "1.0.0" "myapp" "." ""

  assert_failure  # Exits 1 when no SBOMs generated
  assert_output --partial "No container image specified"
  assert_output --partial "No SBOM files generated"
}

# =============================================================================
# Multiple Layers Tests
# =============================================================================

@test "generate-sbomhandles comma-separated layers" {
  create_maven_project

  run_generate_sbom "maven" "source,analyzed-artifact" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Generating Source layer"
  assert_output --partial "Generating Artifact layer"
}

@test "generate-sbomhandles all three layers" {
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  run_generate_sbom "maven" "source,analyzed-artifact,analyzed-container" "1.0.0" "myapp" "." "ghcr.io/org/myapp:1.0.0"

  assert_success
  assert_output --partial "Generating Source layer"
  assert_output --partial "Generating Artifact layer"
  assert_output --partial "Generating Container layer"
}

@test "generate-sbomfails on unknown layer" {
  create_maven_project

  run_generate_sbom "maven" "invalid-layer" "1.0.0" "myapp" "."

  assert_failure
  assert_output --partial "Unknown layer"
}

# =============================================================================
# Version/Name Extraction Tests
# =============================================================================

@test "generate-sbomuses provided version" {
  create_maven_project

  run_generate_sbom "maven" "source" "2.0.0" "myapp" "."

  assert_success
  assert_output --partial "Version: 2.0.0"
}

@test "generate-sbomuses provided project name" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "custom-name" "."

  assert_success
  assert_output --partial "Name: custom-name"
}

# =============================================================================
# Working Directory Tests
# =============================================================================

@test "generate-sbomchanges to working directory" {
  mkdir -p subproject
  cd subproject
  create_maven_project
  cd ..

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "subproject"

  assert_success
}

@test "generate-sbomfails on invalid working directory" {
  run_generate_sbom "maven" "source" "1.0.0" "myapp" "nonexistent"

  assert_failure
}

# =============================================================================
# SBOM Output Tests
# =============================================================================

@test "generate-sbomcreates SPDX format SBOM" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "."

  assert_success
  assert_file_exists "myapp-1.0.0-pom-sbom.spdx.json"
}

@test "generate-sbomcreates CycloneDX format SBOM" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "."

  assert_success
  assert_file_exists "myapp-1.0.0-pom-sbom.cyclonedx.json"
}

@test "generate-sbomcreates dual format SBOMs" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "."

  assert_success
  assert_file_exists "myapp-1.0.0-pom-sbom.spdx.json"
  assert_file_exists "myapp-1.0.0-pom-sbom.cyclonedx.json"
}

# =============================================================================
# ZIP Creation Tests
# =============================================================================

@test "generate-sbomcreates ZIP when requested" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "." "" "true"

  assert_success
  assert_file_exists "myapp-1.0.0-sboms.zip"
}

@test "generate-sbomdoes not create ZIP by default" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "." "" "false"

  assert_success
  assert_file_not_exists "myapp-1.0.0-sboms.zip"
}

# =============================================================================
# Summary Output Tests
# =============================================================================

@test "generate-sbomshows SBOM generation header" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "SBOM Generation Script"
}

@test "generate-sbomshows project information" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Project Information"
  assert_output --partial "Name: myapp"
  assert_output --partial "Version: 1.0.0"
}

@test "generate-sbomshows completion summary" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "SBOM Generation Complete"
  assert_output --partial "Successfully generated"
}

# =============================================================================
# Syft Installation Tests
# =============================================================================

@test "generate-sbomdetects existing syft" {
  create_maven_project
  
  # syft mock already in PATH from setup

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Syft already installed"
}

# =============================================================================
# Error Handling Tests
# =============================================================================

@test "generate-sbomhandles unknown project type gracefully" {
  run_generate_sbom "unknown" "source" "1.0.0" "myapp" "."

  assert_failure  # Exits 1 when no SBOMs generated
  assert_output --partial "Unsupported project type"
  assert_output --partial "No SBOM files generated"
}

# =============================================================================
# Go Project Tests
# =============================================================================

@test "generate-sbomgenerates source layer for go" {
  cat > go.mod << 'EOF'
module github.com/example/myapp

go 1.21
EOF

  run_generate_sbom "go" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Scanning go.mod"
}

# =============================================================================
# Rust Project Tests
# =============================================================================

@test "generate-sbomgenerates source layer for rust" {
  cat > Cargo.toml << 'EOF'
[package]
name = "myapp"
version = "1.0.0"
EOF

  run_generate_sbom "rust" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Scanning Cargo.toml"
}

# =============================================================================
# Python Project Tests
# =============================================================================

@test "generate-sbomgenerates source layer for python with pyproject" {
  cat > pyproject.toml << 'EOF'
[project]
name = "myapp"
version = "1.0.0"
EOF

  run_generate_sbom "python" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Scanning pyproject.toml"
}

@test "generate-sbomgenerates source layer for python with requirements" {
  echo "requests==2.28.0" > requirements.txt

  run_generate_sbom "python" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Scanning requirements.txt"
}
