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
  common_setup
  # Create mock generate-sboms.sh
  MOCK_SBOM_DIR="${TEST_DIR}/mock-sbom"
  mkdir -p "$MOCK_SBOM_DIR"
  cat > "${MOCK_SBOM_DIR}/generate-sboms.sh" <<'SCRIPT'
#!/usr/bin/env bash
printf "called: %s\n" "$*"
SCRIPT
  chmod +x "${MOCK_SBOM_DIR}/generate-sboms.sh"
}

teardown() {
  common_teardown
}

# =============================================================================
# Empty Artifact Types
# =============================================================================

@test "generate-container-sbom handles empty artifact types" {
  run_script "sbom/generate-container-sbom.sh" "" "1.0.0" "myapp" "ghcr.io/org/app@sha256:abc" "$MOCK_SBOM_DIR"

  assert_success
  assert_output --partial "called: --layers analyzed-container --version 1.0.0 --name myapp --container-image ghcr.io/org/app@sha256:abc"
}

@test "generate-container-sbom mentions no artifact dependencies for empty types" {
  run_script "sbom/generate-container-sbom.sh" "" "1.0.0" "myapp" "ghcr.io/org/app@sha256:abc" "$MOCK_SBOM_DIR"

  assert_success
  assert_output --partial "No artifact dependencies"
}

# =============================================================================
# Single Artifact Type
# =============================================================================

@test "generate-container-sbom handles single artifact type" {
  run_script "sbom/generate-container-sbom.sh" "maven" "1.0.0" "myapp" "ghcr.io/org/app@sha256:abc" "$MOCK_SBOM_DIR"

  assert_success
  assert_output --partial "called: --layers analyzed-container --version 1.0.0 --name myapp --container-image ghcr.io/org/app@sha256:abc"
}

@test "generate-container-sbom mentions artifact type name" {
  run_script "sbom/generate-container-sbom.sh" "maven" "1.0.0" "myapp" "ghcr.io/org/app@sha256:abc" "$MOCK_SBOM_DIR"

  assert_success
  assert_output --partial "Generating SBOM for artifact type: maven"
}

# =============================================================================
# Comma-Separated Artifact Types
# =============================================================================

@test "generate-container-sbom handles comma-separated artifact types" {
  run_script "sbom/generate-container-sbom.sh" "maven,npm" "2.0.0" "myapp" "ghcr.io/org/app@sha256:def" "$MOCK_SBOM_DIR"

  assert_success
  assert_output --partial "called: --layers analyzed-container --version 2.0.0 --name myapp --container-image ghcr.io/org/app@sha256:def"
  assert_output --partial "called: --layers analyzed-container --version 2.0.0 --name myapp --container-image ghcr.io/org/app@sha256:def"
}

@test "generate-container-sbom prints each artifact type name" {
  run_script "sbom/generate-container-sbom.sh" "maven,npm,gradle" "1.0.0" "myapp" "ghcr.io/org/app@sha256:abc" "$MOCK_SBOM_DIR"

  assert_success
  assert_output --partial "Generating SBOM for artifact type: maven"
  assert_output --partial "Generating SBOM for artifact type: npm"
  assert_output --partial "Generating SBOM for artifact type: gradle"
}

# =============================================================================
# Argument Passing
# =============================================================================

@test "generate-container-sbom passes correct version and project name" {
  run_script "sbom/generate-container-sbom.sh" "npm" "3.5.1" "my-service" "ghcr.io/org/svc@sha256:xyz" "$MOCK_SBOM_DIR"

  assert_success
  assert_output --partial "called: --layers analyzed-container --version 3.5.1 --name my-service --container-image ghcr.io/org/svc@sha256:xyz"
}

@test "generate-container-sbom passes correct image reference" {
  run_script "sbom/generate-container-sbom.sh" "maven" "1.0.0" "myapp" "registry.example.com/team/app@sha256:deadbeef" "$MOCK_SBOM_DIR"

  assert_success
  assert_output --partial "registry.example.com/team/app@sha256:deadbeef"
}

# =============================================================================
# Success Message
# =============================================================================

@test "generate-container-sbom shows success message" {
  run_script "sbom/generate-container-sbom.sh" "maven" "1.0.0" "myapp" "ghcr.io/org/app@sha256:abc" "$MOCK_SBOM_DIR"

  assert_success
  assert_output --partial "Container SBOM generation completed"
}
