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

# =============================================================================
# Project Detection Tests
# =============================================================================

@test "generate-sbomsdetects maven project from pom.xml" {
  create_maven_project

  # Detection runs before layer generation; exit status isn't relevant here
  # (build layer with no bom.json will fail the summary — we only assert the
  # detection log line).
  run_script "sbom/generate-sboms.sh" \
      --project-type "auto" \
      --layers "build" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_output --partial "Auto-detected project type: maven"
}

@test "generate-sbomsdetects npm project from package.json" {
  create_npm_project

  run_script "sbom/generate-sboms.sh" \
      --project-type "auto" \
      --layers "build" \
      --working-dir "."
  assert_output --partial "Auto-detected project type: npm"
}

@test "generate-sbomsdetects gradle project from build.gradle" {
  create_gradle_project

  run_script "sbom/generate-sboms.sh" \
      --project-type "auto" \
      --layers "build" \
      --working-dir "."
  assert_output --partial "Auto-detected project type: gradle"
}

@test "generate-sbomsdetects go project from go.mod" {
  cat > go.mod << 'EOF'
module github.com/example/myapp

go 1.21
EOF

  run_script "sbom/generate-sboms.sh" \
      --project-type "auto" \
      --layers "build" \
      --working-dir "."
  assert_output --partial "Auto-detected project type: go"
}

@test "generate-sbomsdetects rust project from Cargo.toml" {
  cat > Cargo.toml << 'EOF'
[package]
name = "myapp"
version = "1.0.0"
EOF

  run_script "sbom/generate-sboms.sh" \
      --project-type "auto" \
      --layers "build" \
      --working-dir "."
  assert_output --partial "Auto-detected project type: rust"
}

@test "generate-sbomsdetects python project from pyproject.toml" {
  cat > pyproject.toml << 'EOF'
[project]
name = "myapp"
version = "1.0.0"
EOF

  run_script "sbom/generate-sboms.sh" \
      --project-type "auto" \
      --layers "build" \
      --working-dir "."
  assert_output --partial "Auto-detected project type: python"
}

@test "generate-sbomsdetects python project from requirements.txt" {
  echo "requests==2.28.0" > requirements.txt

  run_script "sbom/generate-sboms.sh" \
      --project-type "auto" \
      --layers "build" \
      --working-dir "."
  assert_output --partial "Auto-detected project type: python"
}

@test "generate-sbomsuses explicit project type over auto-detect" {
  # Create both maven and npm project files
  create_maven_project
  create_npm_project

  run_script "sbom/generate-sboms.sh" \
      --project-type "npm" \
      --layers "build" \
      --working-dir "."
  assert_output --partial "Project type: npm"
  refute_output --partial "Auto-detected"
}


# =============================================================================
# Build Layer Tests
# =============================================================================

@test "generate-sbomsgenerates build layer for maven when bom.json exists" {
  create_maven_project false
  mkdir -p target
  echo '{"bomFormat":"CycloneDX"}' > target/bom.json

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "build" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_output --partial "Generating Build layer"
}

@test "generate-sbomswarns when no bom.json found for maven build layer" {
  create_maven_project false

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "build" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_failure  # Exits 1 when no SBOMs generated (summary check)
  assert_output --partial "No Maven Build SBOM"
  assert_output --partial "No SBOM files generated"
}

@test "generate-sbomsgenerates build layer for npm when bom.json exists" {
  create_npm_project
  echo '{"bomFormat":"CycloneDX"}' >bom.json

  run_script "sbom/generate-sboms.sh" \
      --project-type "npm" \
      --layers "build" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_output --partial "Generating Build layer"
  assert_file_exists "myapp-1.0.0-build-sbom.cyclonedx.json"
}

@test "generate-sbomswarns when no bom.json found for npm build layer" {
  create_npm_project

  run_script "sbom/generate-sboms.sh" \
      --project-type "npm" \
      --layers "build" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_failure
  assert_output --partial "No npm Build SBOM"
}

@test "generate-sbomsgenerates build layer for gradle when bom.json exists" {
  create_gradle_project
  mkdir -p build/reports
  echo '{"bomFormat":"CycloneDX"}' >build/reports/bom.json

  run_script "sbom/generate-sboms.sh" \
      --project-type "gradle" \
      --layers "build" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_output --partial "Generating Build layer"
  assert_file_exists "myapp-1.0.0-build-sbom.cyclonedx.json"
}

@test "generate-sbomswarns when no bom.json found for gradle build layer" {
  create_gradle_project

  run_script "sbom/generate-sboms.sh" \
      --project-type "gradle" \
      --layers "build" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_failure
  assert_output --partial "No Gradle Build SBOM"
}

@test "generate-sbomsgenerates build layer for cargo when bom.json exists" {
  create_rust_project
  echo '{"bomFormat":"CycloneDX"}' >bom.json

  run_script "sbom/generate-sboms.sh" \
      --project-type "rust" \
      --layers "build" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_output --partial "Generating Build layer"
  assert_file_exists "myapp-1.0.0-build-sbom.cyclonedx.json"
}

@test "generate-sbomswarns when no bom.json found for cargo build layer" {
  create_rust_project

  run_script "sbom/generate-sboms.sh" \
      --project-type "rust" \
      --layers "build" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_failure
  assert_output --partial "No Cargo Build SBOM"
}

@test "generate-sbomsbuild layer finds bom.json when working directory is a subdir" {
  # Simulates the post-download-artifact tree when a consumer set
  # working-directory=server/ — upload-artifact preserves the prefix, so the
  # pickup must not assume bom.json lives at the repo root.
  create_maven_project false
  mkdir -p release-artifacts/server/target
  echo '{"bomFormat":"CycloneDX"}' >release-artifacts/server/target/bom.json

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "build" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_output --partial "Generating Build layer"
  assert_file_exists "myapp-1.0.0-build-sbom.cyclonedx.json"
}

@test "generate-sbomsbuild layer prefers aggregate bom over module boms (maven)" {
  # Multi-module Maven: the aggregate BOM lives at target/bom.json (root),
  # per-module BOMs live deeper. Pickup should return the shallowest.
  create_maven_project false
  mkdir -p release-artifacts/target
  echo '{"bomFormat":"CycloneDX","aggregate":true}' >release-artifacts/target/bom.json
  mkdir -p release-artifacts/module-a/target release-artifacts/module-b/target
  echo '{"bomFormat":"CycloneDX","module":"a"}' >release-artifacts/module-a/target/bom.json
  echo '{"bomFormat":"CycloneDX","module":"b"}' >release-artifacts/module-b/target/bom.json

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "build" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_file_exists "myapp-1.0.0-build-sbom.cyclonedx.json"
  grep -q '"aggregate":true' "myapp-1.0.0-build-sbom.cyclonedx.json"
}

@test "generate-sbomsbuild layer ignores npm bom inside node_modules" {
  # Vendored packages can ship their own bom.json — must not hijack pickup.
  create_npm_project
  mkdir -p release-artifacts/node_modules/some-dep
  echo '{"bomFormat":"CycloneDX","vendored":true}' >release-artifacts/node_modules/some-dep/bom.json
  echo '{"bomFormat":"CycloneDX","project":true}' >release-artifacts/bom.json

  run_script "sbom/generate-sboms.sh" \
      --project-type "npm" \
      --layers "build" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  grep -q '"project":true' "myapp-1.0.0-build-sbom.cyclonedx.json"
}

@test "generate-sbomsbuild layer picks shallowest npm bom across depths" {
  # Monorepo / workspaces: root-package bom should beat per-package boms.
  create_npm_project
  mkdir -p release-artifacts/packages/a
  echo '{"bomFormat":"CycloneDX","root":true}' >release-artifacts/bom.json
  echo '{"bomFormat":"CycloneDX","deep":true}' >release-artifacts/packages/a/bom.json

  run_script "sbom/generate-sboms.sh" \
      --project-type "npm" \
      --layers "build" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  grep -q '"root":true' "myapp-1.0.0-build-sbom.cyclonedx.json"
}

@test "generate-sbomsbuild layer prefers root aggregate over module boms (gradle)" {
  # Multi-project Gradle: aggregate bom at the root build/reports/ wins
  # over per-subproject boms.
  create_gradle_project
  mkdir -p release-artifacts/build/reports release-artifacts/module-a/build/reports
  echo '{"bomFormat":"CycloneDX","aggregate":true}' >release-artifacts/build/reports/bom.json
  echo '{"bomFormat":"CycloneDX","module":"a"}' >release-artifacts/module-a/build/reports/bom.json

  run_script "sbom/generate-sboms.sh" \
      --project-type "gradle" \
      --layers "build" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  grep -q '"aggregate":true' "myapp-1.0.0-build-sbom.cyclonedx.json"
}

@test "generate-sbomsbuild layer prefers root-crate bom over sub-crate (rust workspace)" {
  # Rust workspaces: cargo-cyclonedx emits one bom.json per crate root.
  # Root crate should win when aggregating.
  create_rust_project
  mkdir -p release-artifacts/crates/inner
  echo '{"bomFormat":"CycloneDX","root":true}' >release-artifacts/bom.json
  echo '{"bomFormat":"CycloneDX","inner":true}' >release-artifacts/crates/inner/bom.json

  run_script "sbom/generate-sboms.sh" \
      --project-type "rust" \
      --layers "build" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  grep -q '"root":true' "myapp-1.0.0-build-sbom.cyclonedx.json"
}

@test "generate-sbomsbuild layer ignores cargo target cache (rust)" {
  # cargo may leave stale bom.json fragments under target/; exclude them
  # so compile-cache BOMs can't hijack pickup.
  create_rust_project
  mkdir -p release-artifacts/target/debug
  echo '{"bomFormat":"CycloneDX","cached":true}' >release-artifacts/target/debug/bom.json
  echo '{"bomFormat":"CycloneDX","real":true}' >release-artifacts/bom.json

  run_script "sbom/generate-sboms.sh" \
      --project-type "rust" \
      --layers "build" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  grep -q '"real":true' "myapp-1.0.0-build-sbom.cyclonedx.json"
}

@test "generate-sbomsbuild layer included in multi-layer run does not break packaging" {
  create_maven_project true
  mkdir -p target
  echo '{"bomFormat":"CycloneDX"}' > target/bom.json

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "build,analyzed-artifact" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_output --partial "Generating Build layer"
  assert_output --partial "Generating Artifact layer"
}

@test "generate-sbomsmissing build bom.json does not block other layers" {
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "build,analyzed-artifact" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_output --partial "No Maven Build SBOM"
  assert_output --partial "Generating Artifact layer"
}

# =============================================================================
# Artifact Layer Tests
# =============================================================================

@test "generate-sbomsgenerates artifact layer for maven JAR" {
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "analyzed-artifact" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_output --partial "Generating Artifact layer"
}

@test "generate-sbomsgenerates artifact layer for maven JAR without version suffix (finalName)" {
  create_maven_project false
  mkdir -p target
  echo "mock jar" > target/myapp.jar

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "analyzed-artifact" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_output --partial "Generating Artifact layer"
}

@test "generate-sbomswarns when no JARs found for maven" {
  create_maven_project false  # No JAR - script fails when no SBOMs generated

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "analyzed-artifact" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_failure  # Exits 1 when no SBOMs generated
  assert_output --partial "No JAR files found"
  assert_output --partial "No SBOM files generated"
}

@test "generate-sbomsgenerates artifact layer for npm tarball" {
  create_npm_project
  echo "mock tarball" > myapp-1.0.0.tgz

  run_script "sbom/generate-sboms.sh" \
      --project-type "npm" \
      --layers "analyzed-artifact" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_output --partial "Generating Artifact layer"
}

@test "generate-sbomsgenerates artifact layer for gradle" {
  create_gradle_project true

  run_script "sbom/generate-sboms.sh" \
      --project-type "gradle" \
      --layers "analyzed-artifact" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_output --partial "Generating Artifact layer"
}

# =============================================================================
# Container Layer Tests
# =============================================================================

@test "generate-sbomsgenerates container layer when image provided" {
  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "analyzed-container" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "." \
      --container-image "ghcr.io/org/myapp:1.0.0"
  assert_success
  assert_output --partial "Generating Container layer"
  assert_output --partial "ghcr.io/org/myapp:1.0.0"
}

@test "generate-sbomsskips container layer when no image provided" {
  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "analyzed-container" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_failure  # Exits 1 when no SBOMs generated
  assert_output --partial "No container image specified"
  assert_output --partial "No SBOM files generated"
}

# =============================================================================
# Multiple Layers Tests
# =============================================================================

@test "generate-sbomshandles comma-separated layers" {
  create_maven_project true
  mkdir -p target
  echo '{"bomFormat":"CycloneDX"}' > target/bom.json
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "build,analyzed-artifact" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_output --partial "Generating Build layer"
  assert_output --partial "Generating Artifact layer"
}

@test "generate-sbomshandles all three layers" {
  create_maven_project true
  mkdir -p target
  echo '{"bomFormat":"CycloneDX"}' > target/bom.json
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "build,analyzed-artifact,analyzed-container" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "." \
      --container-image "ghcr.io/org/myapp:1.0.0"
  assert_success
  assert_output --partial "Generating Build layer"
  assert_output --partial "Generating Artifact layer"
  assert_output --partial "Generating Container layer"
}

@test "generate-sbomsfails on unknown layer" {
  create_maven_project

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "invalid-layer" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_failure
  assert_output --partial "Unknown layer"
}

# =============================================================================
# Version/Name Extraction Tests
# =============================================================================

@test "generate-sbomsuses provided version" {
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "analyzed-artifact" \
      --version "2.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_output --partial "Version: 2.0.0"
}

@test "generate-sbomsuses provided project name" {
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/custom-name-1.0.0.jar

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "analyzed-artifact" \
      --version "1.0.0" \
      --name "custom-name" \
      --working-dir "."
  assert_success
  assert_output --partial "Name: custom-name"
}

# =============================================================================
# Working Directory Tests
# =============================================================================

@test "generate-sbomschanges to working directory" {
  mkdir -p subproject
  cd subproject
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/
  cd ..

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "analyzed-artifact" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "subproject"
  assert_success
}

@test "generate-sbomsfails on invalid working directory" {
  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "build" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "nonexistent"
  assert_failure
}

# =============================================================================
# SBOM Output Tests
# =============================================================================

@test "generate-sbomscreates SPDX format SBOM" {
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "analyzed-artifact" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_file_exists "myapp-1.0.0-analyzed-jar-sbom.spdx.json"
}

@test "generate-sbomscreates CycloneDX format SBOM" {
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "analyzed-artifact" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_file_exists "myapp-1.0.0-analyzed-jar-sbom.cyclonedx.json"
}

@test "generate-sbomscreates dual format SBOMs" {
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "analyzed-artifact" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_file_exists "myapp-1.0.0-analyzed-jar-sbom.spdx.json"
  assert_file_exists "myapp-1.0.0-analyzed-jar-sbom.cyclonedx.json"
}

# =============================================================================
# ZIP Creation Tests
# =============================================================================

@test "generate-sbomscreates ZIP when requested" {
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "analyzed-artifact" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "." \
      --create-zip
  assert_success
  assert_file_exists "myapp-1.0.0-sboms.zip"
}

@test "generate-sbomsdoes not create ZIP by default" {
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "analyzed-artifact" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_file_not_exists "myapp-1.0.0-sboms.zip"
}

# =============================================================================
# Summary Output Tests
# =============================================================================

@test "generate-sbomsshows SBOM generation header" {
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "analyzed-artifact" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_output --partial "SBOM Generation Script"
}

@test "generate-sbomsshows project information" {
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "analyzed-artifact" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_output --partial "Project Information"
  assert_output --partial "Name: myapp"
  assert_output --partial "Version: 1.0.0"
}

@test "generate-sbomsshows completion summary" {
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "analyzed-artifact" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_output --partial "SBOM Generation Complete"
  assert_output --partial "Successfully generated"
}

# =============================================================================
# Syft Installation Tests
# =============================================================================

@test "generate-sbomsdetects existing syft" {
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  # syft mock already in PATH from setup

  run_script "sbom/generate-sboms.sh" \
      --project-type "maven" \
      --layers "analyzed-artifact" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_success
  assert_output --partial "Syft already installed"
}

# =============================================================================
# Error Handling Tests
# =============================================================================

@test "generate-sbomsrejects unknown project type at flag parse" {
  # --project-type is validated against the allow-list; unknown values fail
  # fast with a clear error rather than silently producing zero SBOMs.
  run_script "sbom/generate-sboms.sh" \
      --project-type "unknown" \
      --layers "build" \
      --version "1.0.0" \
      --name "myapp" \
      --working-dir "."
  assert_failure
  assert_stderr_contains "invalid --project-type 'unknown'"
  assert_stderr_contains "valid:"
}

# =============================================================================
# Flag-parser Tests
# =============================================================================

@test "generate-sboms --help exits 0 with usage text" {
  run_script "sbom/generate-sboms.sh" --help

  assert_success
  assert_output --partial "Usage:"
  assert_output --partial "--project-type"
  assert_output --partial "--layers"
}

@test "generate-sboms -h short flag works" {
  run_script "sbom/generate-sboms.sh" -h

  assert_success
  assert_output --partial "Usage:"
}

@test "generate-sboms rejects unknown flag with usage" {
  run_script "sbom/generate-sboms.sh" --bogus value

  assert_failure
  assert_stderr_contains "unknown flag"
  assert_stderr_contains "Usage:"
}

@test "generate-sboms --project-type without value fails" {
  run_script "sbom/generate-sboms.sh" --project-type

  assert_failure
  assert_stderr_contains "--project-type requires an argument"
}

@test "generate-sboms --layers without value fails" {
  run_script "sbom/generate-sboms.sh" --layers

  assert_failure
  assert_stderr_contains "--layers requires an argument"
}

@test "generate-sboms --create-zip is a boolean flag with no argument" {
  # Verify --create-zip parses cleanly when followed by another flag (not consumed
  # as the flag's value).
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  run_script "sbom/generate-sboms.sh" \
    --create-zip \
    --project-type maven \
    --layers analyzed-artifact \
    --version 1.0.0 \
    --name myapp

  assert_success
  assert_file_exists "myapp-1.0.0-sboms.zip"
}

@test "generate-sboms accepts flags in any order" {
  create_maven_project true
  mkdir -p release-artifacts
  cp target/myapp-1.0.0.jar release-artifacts/

  run_script "sbom/generate-sboms.sh" \
    --layers analyzed-artifact \
    --name myapp \
    --version 1.0.0 \
    --project-type maven

  assert_success
  assert_output --partial "Project type: maven"
  assert_output --partial "Version: 1.0.0"
}
