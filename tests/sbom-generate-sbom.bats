#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2103,SC2119,SC2120,SC2155
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

@test "generate-sbom.sh detects maven project from pom.xml" {
  create_maven_project

  run_generate_sbom "auto" "source" "" "" "."

  assert_success
  assert_output --partial "Auto-detected project type: maven"
}

@test "generate-sbom.sh detects npm project from package.json" {
  create_npm_project

  run_generate_sbom "auto" "source" "" "" "."

  assert_success
  assert_output --partial "Auto-detected project type: npm"
}

@test "generate-sbom.sh detects gradle project from build.gradle" {
  create_gradle_project

  run_generate_sbom "auto" "source" "" "" "."

  assert_success
  assert_output --partial "Auto-detected project type: gradle"
}

@test "generate-sbom.sh detects go project from go.mod" {
  cat > go.mod << 'EOF'
module github.com/example/myapp

go 1.21
EOF

  run_generate_sbom "auto" "source" "" "" "."

  assert_success
  assert_output --partial "Auto-detected project type: go"
}

@test "generate-sbom.sh detects rust project from Cargo.toml" {
  cat > Cargo.toml << 'EOF'
[package]
name = "myapp"
version = "1.0.0"
EOF

  run_generate_sbom "auto" "source" "" "" "."

  assert_success
  assert_output --partial "Auto-detected project type: rust"
}

@test "generate-sbom.sh detects python project from pyproject.toml" {
  cat > pyproject.toml << 'EOF'
[project]
name = "myapp"
version = "1.0.0"
EOF

  run_generate_sbom "auto" "source" "" "" "."

  assert_success
  assert_output --partial "Auto-detected project type: python"
}

@test "generate-sbom.sh detects python project from requirements.txt" {
  echo "requests==2.28.0" > requirements.txt

  run_generate_sbom "auto" "source" "" "" "."

  assert_success
  assert_output --partial "Auto-detected project type: python"
}

@test "generate-sbom.sh uses explicit project type over auto-detect" {
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

@test "generate-sbom.sh generates source layer for maven" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Generating Source layer"
  assert_output --partial "Scanning pom.xml"
}

@test "generate-sbom.sh generates source layer for npm" {
  create_npm_project

  run_generate_sbom "npm" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Generating Source layer"
  assert_output --partial "Scanning package.json"
}

@test "generate-sbom.sh generates source layer for gradle" {
  create_gradle_project

  run_generate_sbom "gradle" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Generating Source layer"
  assert_output --partial "Scanning build.gradle"
}

@test "generate-sbom.sh warns when source manifest missing" {
  # Empty directory, no pom.xml - script fails when no SBOMs generated

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "."

  assert_failure  # Exits 1 when no SBOMs generated
  assert_output --partial "No pom.xml found"
  assert_output --partial "No SBOM files generated"
}

# =============================================================================
# Artifact Layer Tests
# =============================================================================

@test "generate-sbom.sh generates artifact layer for maven JAR" {
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  run_generate_sbom "maven" "artifact" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Generating Artifact layer"
}

@test "generate-sbom.sh warns when no JARs found for maven" {
  create_maven_project false  # No JAR - script fails when no SBOMs generated

  run_generate_sbom "maven" "artifact" "1.0.0" "myapp" "."

  assert_failure  # Exits 1 when no SBOMs generated
  assert_output --partial "No JAR files found"
  assert_output --partial "No SBOM files generated"
}

@test "generate-sbom.sh generates artifact layer for npm tarball" {
  create_npm_project
  echo "mock tarball" > myapp-1.0.0.tgz

  run_generate_sbom "npm" "artifact" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Generating Artifact layer"
}

@test "generate-sbom.sh generates artifact layer for gradle" {
  create_gradle_project true

  run_generate_sbom "gradle" "artifact" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Generating Artifact layer"
}

# =============================================================================
# Container Layer Tests
# =============================================================================

@test "generate-sbom.sh generates container layer when image provided" {
  run_generate_sbom "maven" "containerimage" "1.0.0" "myapp" "." "ghcr.io/org/myapp:1.0.0"

  assert_success
  assert_output --partial "Generating Container layer"
  assert_output --partial "ghcr.io/org/myapp:1.0.0"
}

@test "generate-sbom.sh skips container layer when no image provided" {
  run_generate_sbom "maven" "containerimage" "1.0.0" "myapp" "." ""

  assert_failure  # Exits 1 when no SBOMs generated
  assert_output --partial "No container image specified"
  assert_output --partial "No SBOM files generated"
}

# =============================================================================
# Multiple Layers Tests
# =============================================================================

@test "generate-sbom.sh handles comma-separated layers" {
  create_maven_project

  run_generate_sbom "maven" "source,artifact" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Generating Source layer"
  assert_output --partial "Generating Artifact layer"
}

@test "generate-sbom.sh handles all three layers" {
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  run_generate_sbom "maven" "source,artifact,containerimage" "1.0.0" "myapp" "." "ghcr.io/org/myapp:1.0.0"

  assert_success
  assert_output --partial "Generating Source layer"
  assert_output --partial "Generating Artifact layer"
  assert_output --partial "Generating Container layer"
}

@test "generate-sbom.sh fails on unknown layer" {
  create_maven_project

  run_generate_sbom "maven" "invalid-layer" "1.0.0" "myapp" "."

  assert_failure
  assert_output --partial "Unknown layer"
}

# =============================================================================
# Version/Name Extraction Tests
# =============================================================================

@test "generate-sbom.sh uses provided version" {
  create_maven_project

  run_generate_sbom "maven" "source" "2.0.0" "myapp" "."

  assert_success
  assert_output --partial "Version: 2.0.0"
}

@test "generate-sbom.sh uses provided project name" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "custom-name" "."

  assert_success
  assert_output --partial "Name: custom-name"
}

# =============================================================================
# Working Directory Tests
# =============================================================================

@test "generate-sbom.sh changes to working directory" {
  mkdir -p subproject
  cd subproject
  create_maven_project
  cd ..

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "subproject"

  assert_success
}

@test "generate-sbom.sh fails on invalid working directory" {
  run_generate_sbom "maven" "source" "1.0.0" "myapp" "nonexistent"

  assert_failure
}

# =============================================================================
# SBOM Output Tests
# =============================================================================

@test "generate-sbom.sh creates SPDX format SBOM" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "."

  assert_success
  assert_file_exists "myapp-1.0.0-pom-sbom.spdx.json"
}

@test "generate-sbom.sh creates CycloneDX format SBOM" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "."

  assert_success
  assert_file_exists "myapp-1.0.0-pom-sbom.cyclonedx.json"
}

@test "generate-sbom.sh creates dual format SBOMs" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "."

  assert_success
  assert_file_exists "myapp-1.0.0-pom-sbom.spdx.json"
  assert_file_exists "myapp-1.0.0-pom-sbom.cyclonedx.json"
}

# =============================================================================
# ZIP Creation Tests
# =============================================================================

@test "generate-sbom.sh creates ZIP when requested" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "." "" "true"

  assert_success
  assert_file_exists "myapp-1.0.0-sboms.zip"
}

@test "generate-sbom.sh does not create ZIP by default" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "." "" "false"

  assert_success
  assert_file_not_exists "myapp-1.0.0-sboms.zip"
}

# =============================================================================
# Summary Output Tests
# =============================================================================

@test "generate-sbom.sh shows SBOM generation header" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "SBOM Generation Script"
}

@test "generate-sbom.sh shows project information" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Project Information"
  assert_output --partial "Name: myapp"
  assert_output --partial "Version: 1.0.0"
}

@test "generate-sbom.sh shows completion summary" {
  create_maven_project

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "SBOM Generation Complete"
  assert_output --partial "Successfully generated"
}

# =============================================================================
# Syft Installation Tests
# =============================================================================

@test "generate-sbom.sh detects existing syft" {
  create_maven_project
  
  # syft mock already in PATH from setup

  run_generate_sbom "maven" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Syft already installed"
}

# =============================================================================
# Error Handling Tests
# =============================================================================

@test "generate-sbom.sh handles unknown project type gracefully" {
  run_generate_sbom "unknown" "source" "1.0.0" "myapp" "."

  assert_failure  # Exits 1 when no SBOMs generated
  assert_output --partial "Unsupported project type"
  assert_output --partial "No SBOM files generated"
}

# =============================================================================
# Go Project Tests
# =============================================================================

@test "generate-sbom.sh generates source layer for go" {
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

@test "generate-sbom.sh generates source layer for rust" {
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

@test "generate-sbom.sh generates source layer for python with pyproject" {
  cat > pyproject.toml << 'EOF'
[project]
name = "myapp"
version = "1.0.0"
EOF

  run_generate_sbom "python" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Scanning pyproject.toml"
}

@test "generate-sbom.sh generates source layer for python with requirements" {
  echo "requests==2.28.0" > requirements.txt

  run_generate_sbom "python" "source" "1.0.0" "myapp" "."

  assert_success
  assert_output --partial "Scanning requirements.txt"
}
