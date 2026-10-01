#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/../test_helper.bash"

setup() {
  common_setup_with_isolated_git
  setup_github_env
  init_remote_repo
  # Signing is tested through the real workflow; use annotated tags here.
  # Every push in these tests is confined to the temporary local bare repo.
  local real_git
  real_git="$(command -v git)"
  create_mock_binary "git" "
if [[ \"\${1:-}\" == push ]]; then
  [[ \"\$(\"$real_git\" remote get-url origin)\" == \"\$REMOTE_DIR\" ]] || exit 99
  [[ \"\${MOCK_PUSH_FAIL:-false}\" != true ]] || exit 1
fi
args=()
strip_s=false
for arg in \"\$@\"; do
  if [[ \"\$arg\" == tag ]]; then strip_s=true; fi
  if \$strip_s && [[ \"\$arg\" == -s ]]; then continue; fi
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

prepare_release() {
  TAG_NAME="${1:-v1.0.0}"
  git tag -a "$TAG_NAME" -m "Release request"
  git push -q origin "refs/tags/$TAG_NAME"
  BASE_SHA="$(git rev-parse HEAD)"
  TAG_OBJECT="$(git rev-parse "refs/tags/$TAG_NAME")"
}

bump_release() {
  add_commit "chore(release): $TAG_NAME"
  git push -q origin main
}

run_move_tag() {
  run_script "version/move-tag.sh" "$TAG_NAME" "$BASE_SHA" "$TAG_OBJECT"
}

assert_no_release_output() {
  run get_github_output "release-sha"
  assert_output ""
}

@test "move-tag requires an explicit tag and recorded validation state" {
  run_script "version/move-tag.sh"
  assert_failure
  assert_output --partial "Usage:"
  assert_no_release_output
}

@test "move-tag rejects missing tags" {
  local head
  head="$(git rev-parse HEAD)"
  run_script "version/move-tag.sh" v1.0.0 "$head" "$head"
  assert_failure
  assert_output --partial "Release tag 'v1.0.0' is missing"
  assert_no_release_output
}

@test "move-tag rejects invalid tag names before any update" {
  prepare_release
  TAG_NAME="bad..tag"
  run_move_tag
  assert_failure
  assert_output --partial "Invalid release tag name"
  assert_no_release_output
}

@test "move-tag rejects an unknown base commit" {
  prepare_release
  BASE_SHA="0000000000000000000000000000000000000000"
  run_move_tag
  assert_failure
  assert_output --partial "recorded release base or tag object is missing"
  assert_equal "$(git rev-parse "refs/tags/$TAG_NAME")" "$TAG_OBJECT"
  assert_no_release_output
}

@test "move-tag moves only the requested tag and outputs the release commit" {
  prepare_release
  bump_release
  local head unrelated_tag
  head="$(git rev-parse HEAD)"
  # git describe would choose this HEAD tag instead of the triggering tag.
  git tag -a v9.9.9 -m "Unrelated tag"
  unrelated_tag="$(git rev-parse refs/tags/v9.9.9)"

  run_move_tag

  assert_success
  assert_output --partial "Moving tag v1.0.0"
  assert_equal "$(git rev-parse "refs/tags/$TAG_NAME^{commit}")" "$head"
  assert_equal "$(git --git-dir="$REMOTE_DIR" rev-parse "refs/tags/$TAG_NAME^{commit}")" "$head"
  assert_equal "$(git rev-parse refs/tags/v9.9.9)" "$unrelated_tag"
  run get_github_output "release-sha"
  assert_output "$head"
  run git --git-dir="$REMOTE_DIR" show-ref --verify refs/tags/v9.9.9
  assert_failure # No unrelated tag was pushed.
}

@test "move-tag preserves the signed tag when no release commit was created" {
  prepare_release
  export MOCK_PUSH_FAIL=true # A no-change run must not push.

  run_move_tag

  assert_success
  assert_output --partial "no tag update needed"
  assert_equal "$(git rev-parse "refs/tags/$TAG_NAME")" "$TAG_OBJECT"
  assert_equal "$(git --git-dir="$REMOTE_DIR" rev-parse "refs/tags/$TAG_NAME")" "$TAG_OBJECT"
  run get_github_output "release-sha"
  assert_output "$BASE_SHA"
}

@test "move-tag handles a rerun validated at the already released commit" {
  prepare_release
  bump_release
  run_move_tag
  assert_success
  BASE_SHA="$(git rev-parse HEAD)"
  TAG_OBJECT="$(git rev-parse "refs/tags/$TAG_NAME")"
  export MOCK_PUSH_FAIL=true

  run_move_tag

  assert_success
  assert_output --partial "no tag update needed"
  assert_equal "$(git rev-parse "refs/tags/$TAG_NAME")" "$TAG_OBJECT"
}

@test "move-tag rejects extra commits after the validated base" {
  prepare_release
  add_commit "Unrelated change"
  bump_release

  run_move_tag

  assert_failure
  assert_output --partial "single commit directly after the validated base"
  assert_equal "$(git rev-parse "refs/tags/$TAG_NAME")" "$TAG_OBJECT"
  assert_no_release_output
}

@test "move-tag rejects a merge instead of a version-bump commit" {
  prepare_release
  git switch -q -c feature
  add_commit "Feature change"
  git switch -q main
  git merge -q --no-ff feature -m "Merge feature"

  run_move_tag

  assert_failure
  assert_output --partial "single commit directly after the validated base"
  assert_equal "$(git rev-parse "refs/tags/$TAG_NAME")" "$TAG_OBJECT"
  assert_no_release_output
}

@test "move-tag rejects a local tag changed after validation" {
  prepare_release
  bump_release
  git tag -f -a "$TAG_NAME" -m "Changed tag" HEAD

  run_move_tag

  assert_failure
  assert_output --partial "changed since validation"
  assert_equal "$(git --git-dir="$REMOTE_DIR" rev-parse "refs/tags/$TAG_NAME")" "$TAG_OBJECT"
  assert_no_release_output
}

@test "move-tag rejects a changed tag object even if its commit is unchanged" {
  prepare_release
  git tag -f -a "$TAG_NAME" -m "New annotation" "$BASE_SHA"

  run_move_tag

  assert_failure
  assert_output --partial "changed since validation"
  assert_no_release_output
}

@test "move-tag lease preserves a concurrently changed remote tag" {
  prepare_release
  bump_release
  # Another actor replaces the annotated tag with a lightweight tag at the
  # same commit. A commit-only comparison would miss this change.
  git --git-dir="$REMOTE_DIR" update-ref "refs/tags/$TAG_NAME" "$BASE_SHA"

  run_move_tag

  assert_failure
  assert_equal "$(git --git-dir="$REMOTE_DIR" rev-parse "refs/tags/$TAG_NAME")" "$BASE_SHA"
  assert_no_release_output
}

@test "move-tag rejects a remote tag deleted during a no-change run" {
  prepare_release
  git --git-dir="$REMOTE_DIR" update-ref -d "refs/tags/$TAG_NAME"

  run_move_tag

  assert_failure
  assert_equal "$(git rev-parse "refs/tags/$TAG_NAME")" "$TAG_OBJECT"
  assert_no_release_output
}

@test "move-tag rejects a changed remote tag during a no-change run" {
  prepare_release
  git --git-dir="$REMOTE_DIR" update-ref "refs/tags/$TAG_NAME" "$BASE_SHA"

  run_move_tag

  assert_failure
  assert_output --partial "Remote release tag 'v1.0.0' changed"
  assert_no_release_output
}

@test "move-tag does not emit success when the tag push fails" {
  prepare_release
  bump_release
  export MOCK_PUSH_FAIL=true

  run_move_tag

  assert_failure
  assert_equal "$(git --git-dir="$REMOTE_DIR" rev-parse "refs/tags/$TAG_NAME")" "$TAG_OBJECT"
  assert_no_release_output
}

@test "move-tag handles prerelease names and an explicit tag in a shallow checkout" {
  prepare_release v3.0.0-rc.1
  git fetch -q --depth=1 origin
  assert_equal "$(git rev-parse --is-shallow-repository)" "true"
  bump_release

  run_move_tag

  assert_success
  assert_output --partial "Moving tag v3.0.0-rc.1"
  assert_equal "$(git --git-dir="$REMOTE_DIR" rev-parse "refs/tags/$TAG_NAME^{commit}")" "$(git rev-parse HEAD)"
}

@test "move-tag works without a GitHub output file" {
  prepare_release
  bump_release
  unset GITHUB_OUTPUT

  run_move_tag

  assert_success
  assert_equal "$(git --git-dir="$REMOTE_DIR" rev-parse "refs/tags/$TAG_NAME^{commit}")" "$(git rev-parse HEAD)"
}
