#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-mock/stub.bash"
load "${BATS_TEST_DIRNAME}/../test_helper.bash"

# =============================================================================
# Setup / Teardown
# =============================================================================

setup() {
  # Skip all tests if yq is not installed (required dependency)
  if ! command -v yq &> /dev/null; then
    skip "yq is required but not installed"
  fi
  
  common_setup_with_github_env
  export GITHUB_REF_NAME="v1.0.0"
}

teardown() {
  unstub yq 2>/dev/null || true
  unstub jq 2>/dev/null || true
  # Only cleanup if TEST_DIR was created (skip may have been called)
  [[ -n "${TEST_DIR:-}" ]] && common_teardown || true
}

# =============================================================================
# Config Fixture Functions
# =============================================================================

create_valid_artifacts_config() {
  cat > "$TEST_DIR/artifacts.yml" << 'EOF'
artifacts:
  - name: my-app
    project-type: maven
    build-type: application
    working-directory: .
    publish-to:
      - maven-central
containers: []
EOF
}

create_npm_artifacts_config() {
  cat > "$TEST_DIR/artifacts.yml" << 'EOF'
artifacts:
  - name: my-npm-lib
    project-type: npm
    build-type: library
    working-directory: packages/lib
    publish-to:
      - github-packages
containers: []
EOF
}

create_gradle_artifacts_config() {
  cat > "$TEST_DIR/artifacts.yml" << 'EOF'
artifacts:
  - name: android-app
    project-type: gradle-android
    build-type: application
    working-directory: app
    publish-to:
      - google-play
containers: []
EOF
}

create_multi_artifact_config() {
  cat > "$TEST_DIR/artifacts.yml" << 'EOF'
artifacts:
  - name: backend
    project-type: maven
    build-type: application
    working-directory: backend
  - name: frontend
    project-type: npm
    build-type: application
    working-directory: frontend
containers:
  - name: app-container
    from:
      - backend
      - frontend
    container-file: Containerfile
EOF
}

create_invalid_project_type_config() {
  cat > "$TEST_DIR/artifacts.yml" << 'EOF'
artifacts:
  - name: invalid-app
    project-type: invalid-type
    working-directory: .
containers: []
EOF
}

create_empty_artifacts_config() {
  cat > "$TEST_DIR/artifacts.yml" << 'EOF'
artifacts: []
containers: []
EOF
}

create_multiple_publish_targets_config() {
  cat > "$TEST_DIR/artifacts.yml" << 'EOF'
artifacts:
  - name: multi-publish-lib
    project-type: maven
    build-type: library
    working-directory: .
    publish-to:
      - maven-central
      - github-packages
containers: []
EOF
}

create_meta_project_config() {
  cat > "$TEST_DIR/artifacts.yml" << 'EOF'
artifacts:
  - name: my-meta-project
    project-type: meta
    build-type: meta
    working-directory: .
containers: []
EOF
}

# =============================================================================
# Helper Functions
# =============================================================================

# Run parse-artifacts-config with debug output
run_parse_artifacts_config() {
  run_script "config/parse-artifacts-config.sh"
}

# =============================================================================
# Input Validation Tests
# =============================================================================

@test "parse-artifacts-configfails when config file missing" {
  export ARTIFACTS_CONFIG="$TEST_DIR/nonexistent.yml"

  run_parse_artifacts_config

  assert_failure
  assert_output --partial "File not found"
}

@test "parse-artifacts-configfails on empty artifacts" {
  create_empty_artifacts_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_failure
  assert_output --partial "No artifacts found"
}

@test "parse-artifacts-configfails on invalid project type" {
  create_invalid_project_type_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_failure
  assert_output --partial "Invalid projectType"
  assert_output --partial "invalid-type"
}

# =============================================================================
# Maven Config Tests
# =============================================================================

@test "parse-artifacts-configparses valid maven config" {
  create_valid_artifacts_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_success
  run cat "$GITHUB_OUTPUT"
  assert_output --partial "has-maven=true"
  assert_output --partial "first-project-type=maven"
}

@test "parse-artifacts-configsets has-mavencentral for maven-central publish" {
  create_valid_artifacts_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_success
  run cat "$GITHUB_OUTPUT"
  assert_output --partial "has-mavencentral=true"
}

# =============================================================================
# NPM Config Tests
# =============================================================================

@test "parse-artifacts-configparses valid npm config" {
  create_npm_artifacts_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_success
  run cat "$GITHUB_OUTPUT"
  assert_output --partial "has-npm=true"
  assert_output --partial "first-project-type=npm"
}

@test "parse-artifacts-configsets has-githubpackages for github-packages publish" {
  create_npm_artifacts_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_success
  run cat "$GITHUB_OUTPUT"
  assert_output --partial "has-githubpackages=true"
}

# =============================================================================
# Gradle Config Tests
# =============================================================================

@test "parse-artifacts-configparses valid gradle-android config" {
  create_gradle_artifacts_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_success
  run cat "$GITHUB_OUTPUT"
  # gradle-android is a separate type from plain gradle
  assert_output --partial "has-gradleandroid=true"
  assert_output --partial "first-project-type=gradle-android"
}

@test "parse-artifacts-configsets has-googleplay for google-play publish" {
  create_gradle_artifacts_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_success
  run cat "$GITHUB_OUTPUT"
  assert_output --partial "has-googleplay=true"
}

@test "parse-artifacts-configoutputs googleplay-artifacts with correct content" {
  create_gradle_artifacts_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_success
  run cat "$GITHUB_OUTPUT"
  assert_output --partial "googleplay-artifacts<<"
  assert_output --partial "android-app"
}

@test "parse-artifacts-configsets has-googleplay=false when not publishing to google-play" {
  create_valid_artifacts_config  # Maven config without google-play
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_success
  run cat "$GITHUB_OUTPUT"
  assert_output --partial "has-googleplay=false"
}

# =============================================================================
# Meta Project Tests
# =============================================================================

@test "parse-artifacts-configparses meta project config" {
  create_meta_project_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_success
  run cat "$GITHUB_OUTPUT"
  assert_output --partial "first-project-type=meta"
}

# =============================================================================
# Multi-Artifact Tests
# =============================================================================

@test "parse-artifacts-confighandles multi-artifact config" {
  create_multi_artifact_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_success
  run cat "$GITHUB_OUTPUT"
  assert_output --partial "has-maven=true"
  assert_output --partial "has-npm=true"
}

@test "parse-artifacts-configoutputs containers with artifact types" {
  create_multi_artifact_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_success
  run cat "$GITHUB_OUTPUT"
  assert_output --partial "containers<<"
}

@test "parse-artifacts-confighandles multiple publish targets" {
  create_multiple_publish_targets_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_success
  run cat "$GITHUB_OUTPUT"
  assert_output --partial "has-mavencentral=true"
  assert_output --partial "has-githubpackages=true"
}

# =============================================================================
# SBOM Tests
# =============================================================================

@test "parse-artifacts-configcomputes sbom settings" {
  create_valid_artifacts_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_success
  run cat "$GITHUB_OUTPUT"
  assert_output --partial "needs-sbom="
  assert_output --partial "sbom-artifacts<<"
}

@test "parse-artifacts-configdefaults SBOM on for gradle-android" {
  # gradle-android builders produce a build-SBOM via the cyclonedx-gradle
  # plugin init-script; the default `generate-sbom` should follow, so
  # Android projects get SBOM coverage without explicit opt-in.
  create_gradle_artifacts_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_success
  run cat "$GITHUB_OUTPUT"
  assert_output --partial "needs-sbom=true"
}

# =============================================================================
# Summary Tests
# =============================================================================

@test "parse-artifacts-configgenerates summary" {
  create_valid_artifacts_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_success
  run cat "$GITHUB_STEP_SUMMARY"
  assert_output --partial "Configuration"
  assert_output --partial "my-app"
}

@test "parse-artifacts-configsummary includes all artifacts" {
  create_multi_artifact_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_success
  run cat "$GITHUB_STEP_SUMMARY"
  assert_output --partial "backend"
  assert_output --partial "frontend"
}

# =============================================================================
# Output Variable Tests
# =============================================================================

@test "parse-artifacts-configoutputs first-artifact-name" {
  create_valid_artifacts_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_success
  run cat "$GITHUB_OUTPUT"
  assert_output --partial "first-artifact-name=my-app"
}

@test "parse-artifacts-configoutputs first-build-type" {
  create_multi_artifact_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_success
  run cat "$GITHUB_OUTPUT"
  assert_output --partial "first-build-type=application"
}

@test "parse-artifacts-configoutputs first-project-type for npm" {
  create_npm_artifacts_config
  export ARTIFACTS_CONFIG="$TEST_DIR/artifacts.yml"

  run_parse_artifacts_config

  assert_success
  run cat "$GITHUB_OUTPUT"
  assert_output --partial "first-project-type=npm"
}
