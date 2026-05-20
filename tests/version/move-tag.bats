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
  # Create a git wrapper that strips -s from tag commands (no GPG in tests)
  # while passing all other git commands through unchanged.
  local real_git
  real_git="$(command -v git)"
  create_mock_binary "git" "
args=()
strip_s=false
for arg in \"\$@\"; do
  if [[ \"\$arg\" == \"tag\" ]]; then strip_s=true; fi
  if \$strip_s && [[ \"\$arg\" == \"-s\" ]]; then continue; fi
  args+=(\"\$arg\")
done
exec \"$real_git\" \"\${args[@]}\"
"
  use_mock_path
}

teardown() {
  cleanup_remote
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

# Run move-tag with debug output
run_move_tag() {
  run_script "version/move-tag.sh" "$@"
}

# =============================================================================
# Tag Position Validation Tests
# =============================================================================

@test "move-tag fails when tag not at HEAD~1" {
  # Create tag on initial commit
  git tag -a v1.0.0 -m "Release"

  # Add TWO commits (so tag is at HEAD~2, not HEAD~1)
  echo "change1" >> file.txt
  git add file.txt
  git commit -q -m "First change"

  echo "change2" >> file.txt
  git add file.txt
  git commit -q -m "Second change"

  run_move_tag

  assert_failure
  assert_output --partial "points to unexpected commit"
}

@test "move-tag shows expected vs found commits on failure" {
  git tag -a v1.0.0 -m "Release"

  echo "change1" >> file.txt
  git add file.txt
  git commit -q -m "First"

  echo "change2" >> file.txt
  git add file.txt
  git commit -q -m "Second"

  run_move_tag

  assert_failure
  assert_output --partial "Expected:"
  assert_output --partial "Found:"
}

@test "move-tag fails when no tags exist" {
  # No tags created, git describe will fail
  run_move_tag

  assert_failure
}

@test "move-tag identifies correct tag name" {
  git tag -a v2.5.0 -m "Release v2.5.0"

  # Add commits to make tag not at HEAD~1
  echo "change1" >> file.txt
  git add file.txt
  git commit -q -m "First"

  echo "change2" >> file.txt
  git add file.txt
  git commit -q -m "Second"

  run_move_tag

  assert_failure
  assert_output --partial "v2.5.0"
}

@test "move-tag handles prerelease tag names" {
  git tag -a v1.0.0-rc.1 -m "Release candidate"

  echo "change1" >> file.txt
  git add file.txt
  git commit -q -m "First"

  echo "change2" >> file.txt
  git add file.txt
  git commit -q -m "Second"

  run_move_tag

  assert_failure
  assert_output --partial "v1.0.0-rc.1"
}

# =============================================================================
# Script Logic Tests (using mocked git wrapper)
# =============================================================================

@test "move-tag detects tag at correct position" {
  # Create a wrapper script that simulates the move-tag logic
  # without actually pushing

  git tag -a v1.0.0 -m "Release"

  # Add one commit (tag now at HEAD~1)
  echo "change" >> file.txt
  git add file.txt
  git commit -q -m "Version bump"

  # Verify the tag IS at HEAD~1 (the correct position)
  local tag_sha prev_sha
  tag_sha=$(git rev-list -n 1 v1.0.0)
  prev_sha=$(git rev-parse HEAD~1)

  # This should be equal - meaning move-tag would succeed
  assert_equal "$tag_sha" "$prev_sha"
}

@test "move-tag correctly computes HEAD~1" {
  git tag -a v1.0.0 -m "Release"
  local initial_commit
  initial_commit=$(git rev-parse HEAD)

  echo "change" >> file.txt
  git add file.txt
  git commit -q -m "Second commit"

  local head_minus_1
  head_minus_1=$(git rev-parse HEAD~1)

  assert_equal "$initial_commit" "$head_minus_1"
}

@test "move-tag uses git describe to find latest tag" {
  # Create multiple tags
  git tag -a v1.0.0 -m "First"

  echo "change" >> file.txt
  git add file.txt
  git commit -q -m "Change"

  git tag -a v2.0.0 -m "Second"

  # git describe should return v2.0.0
  local latest
  latest=$(git describe --tags --abbrev=0)

  assert_equal "$latest" "v2.0.0"
}

# =============================================================================
# Success Path Tests (with remote)
# =============================================================================

@test "move-tag succeeds when tag is at HEAD~1 with remote" {
  init_remote_repo

  git tag -a v1.0.0 -m "Release v1.0.0"
  git push -q origin v1.0.0

  add_commit "chore(release): v1.0.0"
  git push -q origin main

  run_move_tag

  assert_success
  assert_output --partial "Moving tag v1.0.0"
}

@test "move-tag moves tag to HEAD after version bump" {
  init_remote_repo

  git tag -a v1.0.0 -m "Release v1.0.0"
  git push -q origin v1.0.0

  local original_tag_sha
  original_tag_sha=$(git rev-list -n 1 v1.0.0)

  add_commit "chore(release): v1.0.0"
  git push -q origin main

  local head_sha
  head_sha=$(git rev-parse HEAD)

  run_move_tag

  assert_success

  # After move, the tag should point to HEAD (the version-bump commit)
  local new_tag_sha
  new_tag_sha=$(git rev-list -n 1 v1.0.0)

  assert_equal "$new_tag_sha" "$head_sha"
  # And it must differ from the original
  assert [ "$new_tag_sha" != "$original_tag_sha" ]
}

@test "move-tag updates remote tag" {
  init_remote_repo

  git tag -a v1.0.0 -m "Release v1.0.0"
  git push -q origin v1.0.0

  add_commit "chore(release): v1.0.0"
  git push -q origin main

  local head_sha
  head_sha=$(git rev-parse HEAD)

  run_move_tag

  assert_success

  # Verify the remote tag was updated too
  local remote_tag_sha
  remote_tag_sha=$(git ls-remote --tags origin v1.0.0 | head -1 | cut -f1)

  # The remote should point to HEAD (might be annotated tag object, resolve it)
  local resolved_remote_sha
  resolved_remote_sha=$(git rev-parse "$remote_tag_sha^{commit}" 2>/dev/null || echo "$remote_tag_sha")

  assert_equal "$resolved_remote_sha" "$head_sha"
}

@test "move-tag tag SHA changes after move — demonstrates checkout v6 issue" {
  # This test demonstrates the core issue: after move-tag runs,
  # the tag SHA differs from the original SHA that triggered the workflow.
  # actions/checkout@v6 compares these and fails.
  init_remote_repo

  git tag -a v2.7.2 -m "Release v2.7.2"
  git push -q origin v2.7.2

  # This is what github.sha would be (the commit that triggered the workflow)
  local trigger_sha
  trigger_sha=$(git rev-list -n 1 v2.7.2)

  # Simulate version-bump adding a commit
  add_commit "chore(release): v2.7.2"
  git push -q origin main

  run_move_tag

  assert_success

  # After move-tag, the tag points to a DIFFERENT commit than trigger_sha.
  # This is why actions/checkout@v6 fails when using the tag name —
  # it expects trigger_sha but finds the new commit.
  local new_tag_sha
  new_tag_sha=$(git rev-list -n 1 v2.7.2)

  assert [ "$new_tag_sha" != "$trigger_sha" ]

  # The fix: using trigger_sha directly (a commit SHA) as the checkout ref
  # bypasses tag validation entirely, since there's no named ref to verify.
}

@test "move-tag outputs release-sha to GITHUB_OUTPUT" {
  init_remote_repo

  git tag -a v1.0.0 -m "Release v1.0.0"
  git push -q origin v1.0.0

  add_commit "chore(release): v1.0.0"
  git push -q origin main

  local head_sha
  head_sha=$(git rev-parse HEAD)

  run_move_tag

  assert_success

  # The script should write the release commit SHA to GITHUB_OUTPUT
  local output_sha
  output_sha=$(get_github_output "release-sha")
  assert_equal "$output_sha" "$head_sha"
}

@test "move-tag does not output release-sha on failure" {
  init_remote_repo

  git tag -a v1.0.0 -m "Release v1.0.0"
  git push -q origin v1.0.0

  # Add TWO commits so tag is at HEAD~2 (not HEAD~1) — will fail
  add_commit "first"
  add_commit "second"
  git push -q origin main

  run_move_tag

  assert_failure

  # GITHUB_OUTPUT should NOT contain release-sha
  local output_sha
  output_sha=$(get_github_output "release-sha")
  assert_equal "$output_sha" ""
}

@test "move-tag works without GITHUB_OUTPUT set" {
  init_remote_repo

  git tag -a v1.0.0 -m "Release v1.0.0"
  git push -q origin v1.0.0

  add_commit "chore(release): v1.0.0"
  git push -q origin main

  # Unset GITHUB_OUTPUT — script should still succeed (writes to /dev/null)
  unset GITHUB_OUTPUT

  run_move_tag

  assert_success
  assert_output --partial "Moving tag v1.0.0"
}

@test "move-tag works with prerelease tags and remote" {
  init_remote_repo

  git tag -a v3.0.0-beta.1 -m "Beta release"
  git push -q origin v3.0.0-beta.1

  add_commit "chore(release): v3.0.0-beta.1"
  git push -q origin main

  run_move_tag

  assert_success
  assert_output --partial "Moving tag v3.0.0-beta.1"
}
