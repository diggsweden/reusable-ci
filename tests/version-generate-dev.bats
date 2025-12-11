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
  common_setup_with_isolated_git
}

teardown() {
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

run_generate_dev_version() {
  run_script "version/generate-dev-version.sh"
}

# =============================================================================
# Basic Output Tests
# =============================================================================

@test "generate-dev-version.sh outputs version string" {
  export GITHUB_REF="refs/heads/main"

  run_generate_dev_version

  assert_success
  assert_output --regexp "^[0-9]+\.[0-9]+\.[0-9]+-dev-"
}

@test "generate-dev-version.sh outputs single line" {
  export GITHUB_REF="refs/heads/main"

  run_generate_dev_version

  assert_success
  # Output should be a single line
  assert [ "$(echo "$output" | wc -l)" -eq 1 ]
}

# =============================================================================
# Version Fallback Tests
# =============================================================================

@test "generate-dev-version.sh uses 0.0.0 when no tags exist" {
  export GITHUB_REF="refs/heads/main"

  run_generate_dev_version

  assert_success
  assert_output --partial "0.0.0-dev-"
}

@test "generate-dev-version.sh uses latest tag version" {
  git tag v1.2.3
  export GITHUB_REF="refs/heads/main"

  run_generate_dev_version

  assert_success
  assert_output --partial "1.2.3-dev-"
}

@test "generate-dev-version.sh uses latest semver tag" {
  git tag v0.1.0
  add_commit "Change 1"
  git tag v0.2.0
  add_commit "Change 2"
  export GITHUB_REF="refs/heads/main"

  run_generate_dev_version

  assert_success
  assert_output --partial "0.2.0-dev-"
}

@test "generate-dev-version.sh ignores non-semver tags" {
  git tag release-candidate
  git tag v1.0.0
  export GITHUB_REF="refs/heads/main"

  run_generate_dev_version

  assert_success
  assert_output --partial "1.0.0-dev-"
}

@test "generate-dev-version.sh ignores lightweight tags without version format" {
  git tag my-feature-tag
  export GITHUB_REF="refs/heads/main"

  run_generate_dev_version

  assert_success
  assert_output --partial "0.0.0-dev-"
}

# =============================================================================
# Branch Name Tests
# =============================================================================

@test "generate-dev-version.sh includes branch name" {
  export GITHUB_REF="refs/heads/feature-branch"

  run_generate_dev_version

  assert_success
  assert_output --partial "-dev-feature-branch-"
}

@test "generate-dev-version.sh sanitizes branch name with slashes" {
  export GITHUB_REF="refs/heads/feature/my-feature"

  run_generate_dev_version

  assert_success
  assert_output --partial "-dev-feature-my-feature-"
  refute_output --partial "/"
}

@test "generate-dev-version.sh sanitizes special characters in branch" {
  export GITHUB_REF="refs/heads/feature@special#chars"

  run_generate_dev_version

  assert_success
  refute_output --partial "@"
  refute_output --partial "#"
}

@test "generate-dev-version.sh handles branch with underscores" {
  export GITHUB_REF="refs/heads/feature_with_underscores"

  run_generate_dev_version

  assert_success
  assert_output --partial "-dev-feature"
}

@test "generate-dev-version.sh handles dependabot branch format" {
  export GITHUB_REF="refs/heads/dependabot/npm_and_yarn/lodash-4.17.21"

  run_generate_dev_version

  assert_success
  # Should sanitize the complex branch name
  refute_output --partial "/"
}

# =============================================================================
# SHA Tests
# =============================================================================

@test "generate-dev-version.sh includes short SHA" {
  export GITHUB_REF="refs/heads/main"
  local short_sha
  short_sha=$(git rev-parse --short=7 HEAD)

  run_generate_dev_version

  assert_success
  assert_output --partial "$short_sha"
}

@test "generate-dev-version.sh SHA is 7 characters" {
  export GITHUB_REF="refs/heads/main"

  run_generate_dev_version

  assert_success
  # Version format: X.Y.Z-dev-branch-SHA
  # The SHA should be 7 characters at the end
  assert_output --regexp "[a-f0-9]{7}$"
}

# =============================================================================
# Version Format Tests
# =============================================================================

@test "generate-dev-version.sh produces valid semver prerelease format" {
  git tag v2.0.0
  export GITHUB_REF="refs/heads/main"

  run_generate_dev_version

  assert_success
  # Should match semver prerelease pattern
  assert_output --regexp "^2\.0\.0-dev-main-[a-f0-9]{7}$"
}

@test "generate-dev-version.sh handles zero-padded version components" {
  git tag v0.0.1
  export GITHUB_REF="refs/heads/main"

  run_generate_dev_version

  assert_success
  assert_output --partial "0.0.1-dev-"
}
