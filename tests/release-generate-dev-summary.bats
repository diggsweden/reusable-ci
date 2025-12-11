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
  common_setup_with_github_env
  export GITHUB_RUN_ID="12345"
}

teardown() {
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

run_generate_dev_summary() {
  run_script "release/generate-dev-summary.sh" "$@"
}

# Standard arguments for testing
# Args: project-type branch commit-sha actor repository container-image container-status npm-name npm-version npm-status
run_with_defaults() {
  run_generate_dev_summary \
    "${1:-maven}" \
    "${2:-main}" \
    "${3:-abc1234567890}" \
    "${4:-test-user}" \
    "${5:-org/repo}" \
    "${6:-}" \
    "${7:-skipped}" \
    "${8:-}" \
    "${9:-}" \
    "${10:-skipped}"
}

# =============================================================================
# Basic Output Tests
# =============================================================================

@test "generate-dev-summary.sh creates step summary" {
  run_with_defaults

  assert_success
  [[ -s "$GITHUB_STEP_SUMMARY" ]]
}

@test "generate-dev-summary.sh outputs dev release header" {
  run_with_defaults

  assert_success
  run get_summary
  assert_output --partial "Dev Release Summary"
}

@test "generate-dev-summary.sh shows success message" {
  run_with_defaults

  assert_success
  assert_output --partial "Dev release summary generated successfully"
}

# =============================================================================
# Build Information Tests
# =============================================================================

@test "generate-dev-summary.sh shows build information section" {
  run_with_defaults

  assert_success
  run get_summary
  assert_output --partial "Build Information"
}

@test "generate-dev-summary.sh shows project type" {
  run_generate_dev_summary "maven" "main" "abc123" "user" "org/repo" "" "skipped" "" "" "skipped"

  assert_success
  run get_summary
  assert_output --partial "Project Type"
  assert_output --partial "maven"
}

@test "generate-dev-summary.sh shows branch name" {
  run_generate_dev_summary "maven" "feature/my-branch" "abc123" "user" "org/repo" "" "skipped" "" "" "skipped"

  assert_success
  run get_summary
  assert_output --partial "Branch"
  assert_output --partial "feature/my-branch"
}

@test "generate-dev-summary.sh shows short commit SHA" {
  run_generate_dev_summary "maven" "main" "abc1234567890" "user" "org/repo" "" "skipped" "" "" "skipped"

  assert_success
  run get_summary
  assert_output --partial "Commit"
  assert_output --partial "abc1234"
}

@test "generate-dev-summary.sh shows actor" {
  run_generate_dev_summary "maven" "main" "abc123" "build-bot" "org/repo" "" "skipped" "" "" "skipped"

  assert_success
  run get_summary
  assert_output --partial "Built By"
  assert_output --partial "@build-bot"
}

@test "generate-dev-summary.sh shows build timestamp" {
  run_with_defaults

  assert_success
  run get_summary
  assert_output --partial "Built At"
}

# =============================================================================
# Job Status Tests
# =============================================================================

@test "generate-dev-summary.sh shows job status section" {
  run_with_defaults

  assert_success
  run get_summary
  assert_output --partial "Job Status"
}

@test "generate-dev-summary.sh shows container job status success" {
  run_generate_dev_summary "maven" "main" "abc123" "user" "org/repo" "ghcr.io/org/app:dev" "success" "" "" "skipped"

  assert_success
  run get_summary
  assert_output --partial "Build Container"
  assert_output --partial "✓"
}

@test "generate-dev-summary.sh shows container job status failure" {
  run_generate_dev_summary "maven" "main" "abc123" "user" "org/repo" "" "failure" "" "" "skipped"

  assert_success
  run get_summary
  assert_output --partial "Build Container"
  assert_output --partial "✗"
}

@test "generate-dev-summary.sh shows container job status skipped" {
  run_generate_dev_summary "maven" "main" "abc123" "user" "org/repo" "" "skipped" "" "" "skipped"

  assert_success
  run get_summary
  assert_output --partial "Build Container"
  assert_output --partial "−"
}

@test "generate-dev-summary.sh shows NPM job status for npm projects" {
  run_generate_dev_summary "npm" "main" "abc123" "user" "org/repo" "" "skipped" "my-package" "1.0.0-dev" "success"

  assert_success
  run get_summary
  assert_output --partial "Publish NPM Package"
  assert_output --partial "✓"
}

@test "generate-dev-summary.sh does not show NPM status for non-npm projects" {
  run_generate_dev_summary "maven" "main" "abc123" "user" "org/repo" "" "skipped" "" "" "skipped"

  assert_success
  run get_summary
  refute_output --partial "Publish NPM Package"
}

# =============================================================================
# Container Artifact Tests
# =============================================================================

@test "generate-dev-summary.sh shows published container image" {
  run_generate_dev_summary "maven" "main" "abc123" "user" "org/repo" "ghcr.io/org/myapp:dev" "success" "" "" "skipped"

  assert_success
  run get_summary
  assert_output --partial "Container Image"
  assert_output --partial "ghcr.io/org/myapp:dev"
}

@test "generate-dev-summary.sh shows docker pull command" {
  run_generate_dev_summary "maven" "main" "abc123" "user" "org/repo" "ghcr.io/org/myapp:dev" "success" "" "" "skipped"

  assert_success
  run get_summary
  assert_output --partial "docker pull ghcr.io/org/myapp:dev"
}

@test "generate-dev-summary.sh shows docker run command" {
  run_generate_dev_summary "maven" "main" "abc123" "user" "org/repo" "ghcr.io/org/myapp:dev" "success" "" "" "skipped"

  assert_success
  run get_summary
  assert_output --partial "docker run ghcr.io/org/myapp:dev"
}

@test "generate-dev-summary.sh shows not published for failed container" {
  run_generate_dev_summary "maven" "main" "abc123" "user" "org/repo" "" "failure" "" "" "skipped"

  assert_success
  run get_summary
  assert_output --partial "Container Image"
  assert_output --partial "Not published"
}

@test "generate-dev-summary.sh shows not published for skipped container" {
  run_generate_dev_summary "maven" "main" "abc123" "user" "org/repo" "" "skipped" "" "" "skipped"

  assert_success
  run get_summary
  assert_output --partial "Not published"
}

# =============================================================================
# NPM Artifact Tests
# =============================================================================

@test "generate-dev-summary.sh shows published NPM package" {
  run_generate_dev_summary "npm" "main" "abc123" "user" "org/repo" "" "skipped" "@org/my-package" "1.0.0-dev.abc123" "success"

  assert_success
  run get_summary
  assert_output --partial "NPM Package"
  assert_output --partial "@org/my-package@1.0.0-dev.abc123"
}

@test "generate-dev-summary.sh shows npm install command for specific version" {
  run_generate_dev_summary "npm" "main" "abc123" "user" "org/repo" "" "skipped" "@org/my-package" "1.0.0-dev.abc123" "success"

  assert_success
  run get_summary
  assert_output --partial "npm install @org/my-package@1.0.0-dev.abc123"
}

@test "generate-dev-summary.sh shows npm install command for dev tag" {
  run_generate_dev_summary "npm" "main" "abc123" "user" "org/repo" "" "skipped" "@org/my-package" "1.0.0-dev.abc123" "success"

  assert_success
  run get_summary
  assert_output --partial "npm install @org/my-package@dev"
}

@test "generate-dev-summary.sh shows not published for failed npm" {
  run_generate_dev_summary "npm" "main" "abc123" "user" "org/repo" "" "skipped" "" "" "failure"

  assert_success
  run get_summary
  assert_output --partial "NPM Package"
  assert_output --partial "Not published"
}

# =============================================================================
# Resources Section Tests
# =============================================================================

@test "generate-dev-summary.sh shows resources section" {
  run_generate_dev_summary "maven" "main" "abc123" "user" "myorg/myrepo" "" "skipped" "" "" "skipped"

  assert_success
  run get_summary
  assert_output --partial "Resources"
}

@test "generate-dev-summary.sh shows packages link" {
  run_generate_dev_summary "maven" "main" "abc123" "user" "myorg/myrepo" "" "skipped" "" "" "skipped"

  assert_success
  run get_summary
  assert_output --partial "github.com/myorg/myrepo/packages"
}

@test "generate-dev-summary.sh shows workflow run link" {
  run_generate_dev_summary "maven" "main" "abc123" "user" "myorg/myrepo" "" "skipped" "" "" "skipped"

  assert_success
  run get_summary
  assert_output --partial "github.com/myorg/myrepo/actions/runs/12345"
}

# =============================================================================
# Development Warning Tests
# =============================================================================

@test "generate-dev-summary.sh shows dev warning note" {
  run_with_defaults

  assert_success
  run get_summary
  assert_output --partial "development artifacts"
  assert_output --partial "Not for production"
}

@test "generate-dev-summary.sh mentions dev tag" {
  run_with_defaults

  assert_success
  run get_summary
  assert_output --partial "dev"
}

# =============================================================================
# Console Output Tests
# =============================================================================

@test "generate-dev-summary.sh shows header in console" {
  run_with_defaults

  assert_success
  assert_output --partial "Generating Dev Release Summary"
}

@test "generate-dev-summary.sh shows project type in console" {
  run_generate_dev_summary "gradle" "main" "abc123" "user" "org/repo" "" "skipped" "" "" "skipped"

  assert_success
  assert_output --partial "Project Type: gradle"
}

@test "generate-dev-summary.sh shows branch in console" {
  run_generate_dev_summary "maven" "develop" "abc123" "user" "org/repo" "" "skipped" "" "" "skipped"

  assert_success
  assert_output --partial "Branch: develop"
}

@test "generate-dev-summary.sh shows commit in console" {
  run_generate_dev_summary "maven" "main" "abc1234" "user" "org/repo" "" "skipped" "" "" "skipped"

  assert_success
  assert_output --partial "Commit: abc1234"
}

@test "generate-dev-summary.sh shows container in console" {
  run_generate_dev_summary "maven" "main" "abc123" "user" "org/repo" "ghcr.io/org/app:dev" "success" "" "" "skipped"

  assert_success
  assert_output --partial "Container Image: ghcr.io/org/app:dev"
}

@test "generate-dev-summary.sh shows NPM package in console" {
  run_generate_dev_summary "npm" "main" "abc123" "user" "org/repo" "" "skipped" "my-pkg" "1.0.0" "success"

  assert_success
  assert_output --partial "NPM Package: my-pkg@1.0.0"
}

# =============================================================================
# Edge Cases
# =============================================================================

@test "generate-dev-summary.sh handles unknown project type" {
  run_generate_dev_summary "unknown" "main" "abc123" "user" "org/repo" "" "skipped" "" "" "skipped"

  assert_success
  run get_summary
  assert_output --partial "unknown"
}

@test "generate-dev-summary.sh handles empty container image" {
  run_generate_dev_summary "maven" "main" "abc123" "user" "org/repo" "" "skipped" "" "" "skipped"

  assert_success
}

@test "generate-dev-summary.sh handles special characters in branch name" {
  run_generate_dev_summary "maven" "feature/JIRA-123" "abc123" "user" "org/repo" "" "skipped" "" "" "skipped"

  assert_success
  run get_summary
  assert_output --partial "feature/JIRA-123"
}

@test "generate-dev-summary.sh handles scoped npm package names" {
  run_generate_dev_summary "npm" "main" "abc123" "user" "org/repo" "" "skipped" "@myorg/my-package" "1.0.0-dev" "success"

  assert_success
  run get_summary
  assert_output --partial "@myorg/my-package"
}

# =============================================================================
# Both Artifacts Published Tests
# =============================================================================

@test "generate-dev-summary.sh shows both container and npm when published" {
  run_generate_dev_summary "npm" "main" "abc123" "user" "org/repo" "ghcr.io/org/app:dev" "success" "@org/app" "1.0.0-dev" "success"

  assert_success
  run get_summary
  assert_output --partial "Container Image"
  assert_output --partial "ghcr.io/org/app:dev"
  assert_output --partial "NPM Package"
  assert_output --partial "@org/app@1.0.0-dev"
}
