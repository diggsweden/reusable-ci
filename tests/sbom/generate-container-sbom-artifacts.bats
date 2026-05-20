#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
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
  common_setup_with_github_env
  export GITHUB_ACTIONS="true"
  export GITHUB_REF_NAME="v1.0.0"
  export GITHUB_REPOSITORY="org/myapp"

  # Create mock generate-container-sbom.sh that records its args
  MOCK_SBOM_DIR="${TEST_DIR}/mock-sbom"
  mkdir -p "$MOCK_SBOM_DIR"
  cat > "${MOCK_SBOM_DIR}/generate-container-sbom.sh" <<'SCRIPT'
#!/usr/bin/env bash
printf "called: %s\n" "$*"
SCRIPT
  chmod +x "${MOCK_SBOM_DIR}/generate-container-sbom.sh"

  # Default env vars
  export ARTIFACT_TYPES="maven"
  export IMAGE_NAME="ghcr.io/org/myapp"
  export IMAGE_DIGEST="sha256:abc123"
  export SBOM_SCRIPT_ROOT="$MOCK_SBOM_DIR"
}

teardown() {
  common_teardown
}

# =============================================================================
# Argument Passing
# =============================================================================

@test "generate-container-sbom-artifacts calls generate-container-sbom.sh with correct args" {
  run_script "sbom/generate-container-sbom-artifacts.sh"

  assert_success
  assert_output --partial "called: maven 1.0.0 myapp ghcr.io/org/myapp@sha256:abc123 $MOCK_SBOM_DIR"
}

# =============================================================================
# Version Stripping
# =============================================================================

@test "generate-container-sbom-artifacts strips v prefix from version" {
  export GITHUB_REF_NAME="v2.5.3"

  run_script "sbom/generate-container-sbom-artifacts.sh"

  assert_success
  assert_output --partial "called: maven 2.5.3 myapp"
}

@test "generate-container-sbom-artifacts handles version without v prefix" {
  export GITHUB_REF_NAME="3.0.0"

  run_script "sbom/generate-container-sbom-artifacts.sh"

  assert_success
  assert_output --partial "called: maven 3.0.0 myapp"
}

# =============================================================================
# Project Name Derivation
# =============================================================================

@test "generate-container-sbom-artifacts derives project name from repository basename" {
  export GITHUB_REPOSITORY="my-org/my-service"

  run_script "sbom/generate-container-sbom-artifacts.sh"

  assert_success
  assert_output --partial "my-service"
}

# =============================================================================
# Image Construction
# =============================================================================

@test "generate-container-sbom-artifacts constructs image from IMAGE_NAME and IMAGE_DIGEST" {
  export IMAGE_NAME="registry.example.com/team/app"
  export IMAGE_DIGEST="sha256:deadbeef0123"

  run_script "sbom/generate-container-sbom-artifacts.sh"

  assert_success
  assert_output --partial "registry.example.com/team/app@sha256:deadbeef0123"
}

# =============================================================================
# Script Root Passing
# =============================================================================

@test "generate-container-sbom-artifacts passes SBOM_SCRIPT_ROOT as script dir" {
  run_script "sbom/generate-container-sbom-artifacts.sh"

  assert_success
  assert_output --partial "$MOCK_SBOM_DIR"
}
