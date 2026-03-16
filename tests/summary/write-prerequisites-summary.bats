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
  common_setup_with_isolated_git
  setup_github_env

  # Create an annotated tag
  git tag -a "v1.0.0" -m "Release v1.0.0"
}

teardown() {
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

run_write_prerequisites_summary() {
  run_script "summary/write-prerequisites-summary.sh" "$@"
}

# Set standard environment variables
set_standard_env() {
  export TAG_NAME="v1.0.0"
  export COMMIT_SHA=$(git rev-parse HEAD)
  export REF_TYPE="tag"
  export PROJECT_TYPE="maven"
  export BUILD_TYPE="application"
  export CONTAINER_REGISTRY="ghcr.io"
  export SIGN_ARTIFACTS="false"
  export CHECK_AUTHORIZATION="false"
  export ACTOR="test-user"
  export JOB_STATUS="success"
}

# =============================================================================
# Basic Output Tests
# =============================================================================

@test "write-prerequisites-summary creates step summary" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  [[ -s "$GITHUB_STEP_SUMMARY" ]]
}

@test "write-prerequisites-summary outputs report header" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Release Prerequisites Validation Report"
}

# =============================================================================
# Tag Information Tests
# =============================================================================

@test "write-prerequisites-summary shows tag name" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "v1.0.0"
}

@test "write-prerequisites-summary shows tag type" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Release Tag"
  assert_output --partial "tag"
}

@test "write-prerequisites-summary shows tagger info" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Tagger"
}

@test "write-prerequisites-summary shows tag message" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Tag Message"
}

# =============================================================================
# Commit Information Tests
# =============================================================================

@test "write-prerequisites-summary shows commit SHA" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "SHA"
  assert_output --partial "$COMMIT_SHA"
}

@test "write-prerequisites-summary shows commit author" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Author"
}

@test "write-prerequisites-summary shows commit date" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Date"
}

@test "write-prerequisites-summary shows commit message" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Message"
}

# =============================================================================
# Configuration Section Tests
# =============================================================================

@test "write-prerequisites-summary shows configuration section" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Configuration"
}

@test "write-prerequisites-summary shows project type" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Project Type"
  assert_output --partial "maven"
}

@test "write-prerequisites-summary shows build type" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Build Type"
  assert_output --partial "application"
}

@test "write-prerequisites-summary shows container registry" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Container Registry"
  assert_output --partial "ghcr.io"
}

@test "write-prerequisites-summary shows GPG signing status disabled" {
  set_standard_env
  export SIGN_ARTIFACTS="false"

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "GPG Signing"
  assert_output --partial "Disabled"
}

@test "write-prerequisites-summary shows GPG signing status enabled" {
  set_standard_env
  export SIGN_ARTIFACTS="true"

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "GPG Signing"
  assert_output --partial "Enabled"
}

# =============================================================================
# Secrets Status Tests
# =============================================================================

@test "write-prerequisites-summary shows secrets status section" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Required Secrets Status"
}

@test "write-prerequisites-summary shows missing release bot token" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "RELEASE_BOT_TOKEN"
  assert_output --partial "Missing"
}

@test "write-prerequisites-summary shows available release bot token" {
  set_standard_env
  export HAS_RELEASE_BOT_TOKEN="true"

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "RELEASE_BOT_TOKEN"
  assert_output --partial "Available"
}

@test "write-prerequisites-summary shows GPG secrets when signing enabled" {
  set_standard_env
  export SIGN_ARTIFACTS="true"

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "OSPO_BOT_GPG_PRIV"
  assert_output --partial "OSPO_BOT_GPG_PASS"
}

# =============================================================================
# Job Status Tests
# =============================================================================

@test "write-prerequisites-summary shows success status" {
  set_standard_env
  export JOB_STATUS="success"

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "All required prerequisites are configured"
}

@test "write-prerequisites-summary shows failure status" {
  set_standard_env
  export JOB_STATUS="failure"

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Prerequisites validation failed"
}

# =============================================================================
# Validation Results Tests
# =============================================================================

@test "write-prerequisites-summary shows validation results section" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Validation Results"
}

@test "write-prerequisites-summary validates semantic version" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Semantic Version"
  assert_output --partial "Pass"
}

@test "write-prerequisites-summary detects stable release" {
  set_standard_env
  export TAG_NAME="v1.0.0"

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Stable"
}

@test "write-prerequisites-summary detects alpha prerelease" {
  set_standard_env
  export TAG_NAME="v1.0.0-alpha"
  git tag -a "v1.0.0-alpha" -m "Alpha release"

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Pre-release"
}

@test "write-prerequisites-summary detects beta prerelease" {
  set_standard_env
  export TAG_NAME="v1.0.0-beta.1"
  git tag -a "v1.0.0-beta.1" -m "Beta release"

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Pre-release"
}

@test "write-prerequisites-summary detects SNAPSHOT prerelease" {
  set_standard_env
  export TAG_NAME="v1.0.0-SNAPSHOT"
  git tag -a "v1.0.0-SNAPSHOT" -m "Snapshot release"

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "SNAPSHOT"
}

# =============================================================================
# Authorization Tests
# =============================================================================

@test "write-prerequisites-summary shows authorization check when enabled" {
  set_standard_env
  export CHECK_AUTHORIZATION="true"

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "User Authorization"
  assert_output --partial "authorized"
}

@test "write-prerequisites-summary skips authorization for SNAPSHOT" {
  set_standard_env
  export TAG_NAME="v1.0.0-SNAPSHOT"
  export CHECK_AUTHORIZATION="false"
  git tag -a "v1.0.0-SNAPSHOT" -m "Snapshot"

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Skip"
  assert_output --partial "SNAPSHOT"
}

# =============================================================================
# Publishing Credentials Tests
# =============================================================================

@test "write-prerequisites-summary checks Maven Central credentials" {
  set_standard_env
  export PUBLISH_TO="maven-central"

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Maven Central"
  assert_output --partial "MAVENCENTRAL_USERNAME"
}

@test "write-prerequisites-summary checks NPM credentials" {
  set_standard_env
  export PUBLISH_TO="npmjs"

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "NPM"
  assert_output --partial "NPM_TOKEN"
}

@test "write-prerequisites-summary checks GitHub Packages" {
  set_standard_env
  export PUBLISH_TO="github-packages"

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "GitHub Packages"
}

# =============================================================================
# Footer Tests
# =============================================================================

@test "write-prerequisites-summary shows footer with timestamp" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "Generated at"
  assert_output --partial "UTC"
}

# =============================================================================
# Edge Cases
# =============================================================================

@test "write-prerequisites-summary handles branch ref type" {
  set_standard_env
  export REF_TYPE="branch"

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "branch"
}

@test "write-prerequisites-summary handles missing environment variables" {
  # Only set required vars
  export TAG_NAME=""
  export COMMIT_SHA=$(git rev-parse HEAD)
  export REF_TYPE=""
  export PROJECT_TYPE=""
  export BUILD_TYPE=""
  export CONTAINER_REGISTRY=""
  export SIGN_ARTIFACTS="false"
  export CHECK_AUTHORIZATION="false"
  export ACTOR=""
  export JOB_STATUS="success"

  run_write_prerequisites_summary

  assert_success
}

# =============================================================================
# Markdown Table Tests
# =============================================================================

@test "write-prerequisites-summary creates proper markdown tables" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  # Check for markdown table separators
  assert_output --partial "|-----"
}

@test "write-prerequisites-summary creates secrets table" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "| Secret | Purpose | Status |"
}

@test "write-prerequisites-summary creates validation table" {
  set_standard_env

  run_write_prerequisites_summary

  assert_success
  run get_summary
  assert_output --partial "| Validation | Result | Details |"
}
