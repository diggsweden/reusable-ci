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
  common_setup
}

teardown() {
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

run_create_release() {
  run_script "release/create-github-release.sh" "$@"
}

# Create a mock gh command that records calls
create_gh_release_mock() {
  local release_view_behavior="${1:-exit 1}"  # Default: no existing release
  local release_create_behavior="${2:-true}"   # Default: success
  local release_delete_behavior="${3:-true}"   # Default: success
  
  create_mock_binary "gh" "
# Record all calls for verification
echo \"\$@\" >> '${TEST_DIR}/gh_calls.log'

case \"\$1\" in
  release)
    case \"\$2\" in
      view)
        $release_view_behavior
        ;;
      create)
        $release_create_behavior
        ;;
      delete)
        $release_delete_behavior
        ;;
    esac
    ;;
esac
"
  use_mock_path
}

# Create gh mock that simulates existing draft release
create_gh_draft_release_mock() {
  create_mock_binary "gh" "
echo \"\$@\" >> '${TEST_DIR}/gh_calls.log'

case \"\$1\" in
  release)
    case \"\$2\" in
      view)
        printf '{\"isDraft\": true, \"isPrerelease\": false}'
        ;;
      create)
        printf 'Release created'
        ;;
      delete)
        printf 'Deleted'
        ;;
    esac
    ;;
esac
"
  use_mock_path
}

# Create gh mock that simulates existing prerelease
create_gh_prerelease_mock() {
  create_mock_binary "gh" "
echo \"\$@\" >> '${TEST_DIR}/gh_calls.log'

case \"\$1\" in
  release)
    case \"\$2\" in
      view)
        printf '{\"isDraft\": false, \"isPrerelease\": true}'
        ;;
      create)
        printf 'Release created'
        ;;
      delete)
        printf 'Deleted'
        ;;
    esac
    ;;
esac
"
  use_mock_path
}

# Create gh mock that simulates existing production release (should fail)
create_gh_production_release_mock() {
  create_mock_binary "gh" "
echo \"\$@\" >> '${TEST_DIR}/gh_calls.log'

case \"\$1\" in
  release)
    case \"\$2\" in
      view)
        printf '{\"isDraft\": false, \"isPrerelease\": false}'
        ;;
    esac
    ;;
esac
"
  use_mock_path
}

# =============================================================================
# Input Validation Tests
# =============================================================================

@test "create-github-release.sh requires tag-name argument" {
  create_gh_release_mock

  run_create_release

  assert_failure
  assert_stderr_contains "Usage"
}

@test "create-github-release.sh requires repository argument" {
  create_gh_release_mock

  run_create_release "v1.0.0"

  assert_failure
  assert_stderr_contains "Usage"
}

# =============================================================================
# Basic Release Creation Tests
# =============================================================================

@test "create-github-release.sh creates release with tag name" {
  create_gh_release_mock

  run_create_release "v1.0.0" "owner/repo" "false" "true" ""

  assert_success
  
  # Verify gh release create was called with tag
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "release create"
  assert_output --partial "v1.0.0"
}

@test "create-github-release.sh sets release title to tag name" {
  create_gh_release_mock

  run_create_release "v2.0.0" "owner/repo" "false" "true" ""

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "--title"
  assert_output --partial "v2.0.0"
}

# =============================================================================
# Draft Release Tests
# =============================================================================

@test "create-github-release.sh creates draft release when draft=true" {
  create_gh_release_mock

  run_create_release "v1.0.0" "owner/repo" "true" "true" ""

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "--draft"
}

@test "create-github-release.sh does not add --draft flag when draft=false" {
  create_gh_release_mock

  run_create_release "v1.0.0" "owner/repo" "false" "true" ""

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  refute_output --partial "--draft"
}

# =============================================================================
# Prerelease Detection Tests
# =============================================================================

@test "create-github-release.sh marks alpha as prerelease" {
  create_gh_release_mock

  run_create_release "v1.0.0-alpha" "owner/repo" "false" "true" ""

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "--prerelease"
}

@test "create-github-release.sh marks beta as prerelease" {
  create_gh_release_mock

  run_create_release "v1.0.0-beta.1" "owner/repo" "false" "true" ""

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "--prerelease"
}

@test "create-github-release.sh marks rc as prerelease" {
  create_gh_release_mock

  run_create_release "v1.0.0-rc.2" "owner/repo" "false" "true" ""

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "--prerelease"
}

@test "create-github-release.sh marks dev as prerelease" {
  create_gh_release_mock

  run_create_release "v1.0.0-dev" "owner/repo" "false" "true" ""

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "--prerelease"
}

@test "create-github-release.sh marks snapshot as prerelease" {
  create_gh_release_mock

  run_create_release "v1.0.0-snapshot" "owner/repo" "false" "true" ""

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "--prerelease"
}

@test "create-github-release.sh does not mark stable release as prerelease" {
  create_gh_release_mock

  run_create_release "v1.0.0" "owner/repo" "false" "true" ""

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  refute_output --partial "--prerelease"
}

# =============================================================================
# Make Latest Tests
# =============================================================================

@test "create-github-release.sh sets latest=false when make-latest is not true" {
  create_gh_release_mock

  run_create_release "v1.0.0" "owner/repo" "false" "false" ""

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "--latest=false"
}

@test "create-github-release.sh does not add latest flag when make-latest=true" {
  create_gh_release_mock

  run_create_release "v1.0.0" "owner/repo" "false" "true" ""

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  refute_output --partial "--latest"
}

# =============================================================================
# Release Notes Tests
# =============================================================================

@test "create-github-release.sh uses release notes file when present" {
  create_gh_release_mock
  echo "# Release Notes" > release-notes.md
  echo "- Feature 1" >> release-notes.md

  run_create_release "v1.0.0" "owner/repo" "false" "true" ""

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "--notes-file"
  assert_output --partial "release-notes.md"
}

@test "create-github-release.sh uses custom release notes file" {
  create_gh_release_mock
  echo "# Custom Notes" > custom-notes.md

  run_create_release "v1.0.0" "owner/repo" "false" "true" "" "custom-notes.md"

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "custom-notes.md"
}

@test "create-github-release.sh skips empty release notes file" {
  create_gh_release_mock
  touch release-notes.md  # Empty file

  run_create_release "v1.0.0" "owner/repo" "false" "true" ""

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  refute_output --partial "--notes-file"
}

@test "create-github-release.sh handles missing release notes file" {
  create_gh_release_mock
  # No release-notes.md created

  run_create_release "v1.0.0" "owner/repo" "false" "true" ""

  assert_success
}

# =============================================================================
# Artifact Attachment Tests
# =============================================================================

@test "create-github-release.sh attaches files from release-artifacts directory" {
  create_gh_release_mock
  create_release_artifacts "myapp" "1.0.0"

  run_create_release "v1.0.0" "owner/repo" "false" "true" ""

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "myapp-1.0.0.jar"
}

@test "create-github-release.sh attaches files matching pattern" {
  create_gh_release_mock
  echo "custom artifact" > custom-file.txt

  run_create_release "v1.0.0" "owner/repo" "false" "true" "custom-file.txt"

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "custom-file.txt"
}

@test "create-github-release.sh handles comma-separated patterns" {
  create_gh_release_mock
  echo "file1" > file1.txt
  echo "file2" > file2.md

  run_create_release "v1.0.0" "owner/repo" "false" "true" "file1.txt,file2.md"

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "file1.txt"
  assert_output --partial "file2.md"
}

@test "create-github-release.sh attaches signature files" {
  create_gh_release_mock
  echo "content" > myfile.txt
  echo "signature" > myfile.txt.asc

  run_create_release "v1.0.0" "owner/repo" "false" "true" "myfile.txt"

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "myfile.txt"
  assert_output --partial "myfile.txt.asc"
}

# =============================================================================
# SBOM Artifact Tests
# =============================================================================

@test "create-github-release.sh attaches SBOM zip when present" {
  create_gh_release_mock
  echo "sbom content" > "myapp-1.0.0-sboms.zip"

  run_create_release "v1.0.0" "owner/repo" "false" "true" "" "release-notes.md" "myapp"

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "myapp-1.0.0-sboms.zip"
}

@test "create-github-release.sh warns when SBOM zip missing" {
  create_gh_release_mock
  # No SBOM zip created

  run_create_release "v1.0.0" "owner/repo" "false" "true" "" "release-notes.md" "myapp"

  assert_success
  assert_output --partial "warning"
  assert_output --partial "SBOM ZIP not found"
}

# =============================================================================
# Checksum Artifact Tests
# =============================================================================

@test "create-github-release.sh attaches checksums.sha256 when present" {
  create_gh_release_mock
  create_checksums_file "checksums.sha256"

  run_create_release "v1.0.0" "owner/repo" "false" "true" ""

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "checksums.sha256"
}

@test "create-github-release.sh attaches checksum signature when present" {
  create_gh_release_mock
  create_checksums_file "checksums.sha256"
  echo "sig" > checksums.sha256.asc

  run_create_release "v1.0.0" "owner/repo" "false" "true" ""

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "checksums.sha256.asc"
}

@test "create-github-release.sh warns when checksums empty" {
  create_gh_release_mock
  touch checksums.sha256  # Empty file

  run_create_release "v1.0.0" "owner/repo" "false" "true" ""

  assert_success
  assert_output --partial "warning"
  assert_output --partial "checksums.sha256"
}

# =============================================================================
# Existing Release Cleanup Tests
# =============================================================================

@test "create-github-release.sh deletes existing draft before creating" {
  create_gh_draft_release_mock

  run_create_release "v1.0.0" "owner/repo" "false" "true" ""

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "release delete"
  assert_output --partial "release create"
}

@test "create-github-release.sh deletes existing prerelease before creating" {
  create_gh_prerelease_mock

  run_create_release "v1.0.0" "owner/repo" "false" "true" ""

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "release delete"
}

@test "create-github-release.sh fails when production release exists" {
  create_gh_production_release_mock

  run_create_release "v1.0.0" "owner/repo" "false" "true" ""

  assert_failure
  assert_output --partial "error"
  assert_output --partial "already exists"
  assert_output --partial "not a draft/prerelease"
}

@test "create-github-release.sh proceeds when no existing release" {
  create_gh_release_mock "exit 1"  # gh release view fails = no release exists

  run_create_release "v1.0.0" "owner/repo" "false" "true" ""

  assert_success
  
  run cat "$TEST_DIR/gh_calls.log"
  refute_output --partial "release delete"
}

# =============================================================================
# Artifact Deduplication Tests
# =============================================================================

@test "create-github-release.sh does not duplicate files" {
  create_gh_release_mock
  mkdir -p release-artifacts
  echo "jar" > release-artifacts/myapp.jar
  # Also match via pattern
  
  run_create_release "v1.0.0" "owner/repo" "false" "true" "release-artifacts/myapp.jar"

  assert_success
  
  # Count occurrences of myapp.jar - should only appear once in args
  run bash -c "grep -o 'myapp.jar' '${TEST_DIR}/gh_calls.log' | wc -l"
  [[ "${output//[[:space:]]/}" -le "2" ]]  # At most 2 (jar + potential .asc)
}

# =============================================================================
# Version Extraction Tests
# =============================================================================

@test "create-github-release.sh extracts version without v prefix" {
  create_gh_release_mock
  echo "content" > "myapp-1.0.0-sboms.zip"

  run_create_release "v1.0.0" "owner/repo" "false" "true" "" "release-notes.md" "myapp"

  assert_success
  # The version 1.0.0 (without v) is used in SBOM zip name
  run cat "$TEST_DIR/gh_calls.log"
  assert_output --partial "myapp-1.0.0-sboms.zip"
}

# =============================================================================
# Error Handling Tests  
# =============================================================================

@test "create-github-release.sh handles gh command failure" {
  create_mock_binary "gh" "
case \"\$1\" in
  release)
    case \"\$2\" in
      view)
        exit 1
        ;;
      create)
        printf '::error::Failed to create release\n' >&2
        exit 1
        ;;
    esac
    ;;
esac
"
  use_mock_path

  run_create_release "v1.0.0" "owner/repo" "false" "true" ""

  assert_failure
}
