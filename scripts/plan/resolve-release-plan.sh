#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"
source "$SCRIPT_DIR/../ci/env.sh"

is_stable_release() {
  local release_type="$1"
  local ref_name="$2"

  if [[ "$release_type" = "stable" ]] || { [[ -z "$release_type" ]] && [[ "$ref_name" != *-* ]]; }; then
    printf "true\n"
  else
    printf "false\n"
  fi
}

has_containers() {
  local containers="$1"

  if [[ "$containers" != '[]' ]]; then
    printf "true\n"
  else
    printf "false\n"
  fi
}

should_generate_sbom() {
  local release_generate_sbom="$1"
  local needs_sbom="$2"

  if [[ "$release_generate_sbom" = "true" ]] && [[ "$needs_sbom" = "true" ]]; then
    printf "true\n"
  else
    printf "false\n"
  fi
}

should_sign_artifacts() {
  local release_sign_artifacts="$1"

  if [[ "$release_sign_artifacts" = "true" ]]; then
    printf "true\n"
  else
    printf "false\n"
  fi
}

should_create_release() {
  local release_publisher="$1"

  if [[ "$release_publisher" = "github-cli" ]]; then
    printf "true\n"
  else
    printf "false\n"
  fi
}

should_check_authorization() {
  local require_authorization="$1"
  local release_check_authorization="$2"

  if [[ "$require_authorization" = "true" ]] || [[ "$release_check_authorization" = "true" ]]; then
    printf "true\n"
  else
    printf "false\n"
  fi
}

should_run_version_bump() {
  local changelog_skip_version_bump="$1"
  local changelog_creator="$2"

  if [[ "$changelog_skip_version_bump" != "true" ]] && [[ "$changelog_creator" = "git-cliff" ]]; then
    printf "true\n"
  else
    printf "false\n"
  fi
}

is_draft_release() {
  local ref_name="$1"

  if ci_is_snapshot "$ref_name"; then
    printf "true\n"
  elif ! ci_is_semver_tag "$ref_name"; then
    printf "true\n"
  else
    printf "false\n"
  fi
}

should_create_draft_release() {
  local release_draft="$1"
  local draft_release="$2"

  if [[ "$release_draft" = "true" ]] || [[ "$draft_release" = "true" ]]; then
    printf "true\n"
  else
    printf "false\n"
  fi
}

write_outputs() {
  local stable_release="$1"
  local containers_present="$2"
  local generate_sbom="$3"
  local sign_artifacts="$4"
  local create_release="$5"
  local check_authorization="$6"
  local run_version_bump="$7"
  local create_draft_release="$8"

  ci_output "should-make-latest" "$stable_release"
  ci_output "has-containers" "$containers_present"
  ci_output "should-generate-sbom" "$generate_sbom"
  ci_output "should-sign-artifacts" "$sign_artifacts"
  ci_output "should-create-release" "$create_release"
  ci_output "should-check-authorization" "$check_authorization"
  ci_output "should-run-version-bump" "$run_version_bump"
  ci_output "should-create-draft-release" "$create_draft_release"
}

main() {
  readonly RELEASE_TYPE="${RELEASE_TYPE:-}"
  readonly RELEASE_PUBLISHER="${RELEASE_PUBLISHER:-}"
  readonly RELEASE_CHECK_AUTHORIZATION="${RELEASE_CHECK_AUTHORIZATION:-false}"
  readonly RELEASE_DRAFT="${RELEASE_DRAFT:-false}"
  readonly RELEASE_GENERATE_SBOM="${RELEASE_GENERATE_SBOM:-false}"
  readonly RELEASE_SIGN_ARTIFACTS="${RELEASE_SIGN_ARTIFACTS:-false}"
  readonly CHANGELOG_CREATOR="${CHANGELOG_CREATOR:-}"
  readonly CHANGELOG_SKIP_VERSION_BUMP="${CHANGELOG_SKIP_VERSION_BUMP:-false}"
  readonly CI_REF_NAME="${CI_REF_NAME:?CI_REF_NAME is required}"
  readonly NEEDS_SBOM="${NEEDS_SBOM:-false}"
  readonly FIRST_REQUIRE_AUTHORIZATION="${FIRST_REQUIRE_AUTHORIZATION:-false}"
  readonly CONTAINERS="${CONTAINERS:-[]}"

  local stable_release
  local containers_present
  local generate_sbom
  local sign_artifacts
  local create_release
  local check_authorization
  local run_version_bump
  local draft_release
  local create_draft_release

  stable_release=$(is_stable_release "$RELEASE_TYPE" "$CI_REF_NAME")
  containers_present=$(has_containers "$CONTAINERS")
  generate_sbom=$(should_generate_sbom "$RELEASE_GENERATE_SBOM" "$NEEDS_SBOM")
  sign_artifacts=$(should_sign_artifacts "$RELEASE_SIGN_ARTIFACTS")
  create_release=$(should_create_release "$RELEASE_PUBLISHER")
  check_authorization=$(should_check_authorization "$FIRST_REQUIRE_AUTHORIZATION" "$RELEASE_CHECK_AUTHORIZATION")
  run_version_bump=$(should_run_version_bump "$CHANGELOG_SKIP_VERSION_BUMP" "$CHANGELOG_CREATOR")
  draft_release=$(is_draft_release "$CI_REF_NAME")
  create_draft_release=$(should_create_draft_release "$RELEASE_DRAFT" "$draft_release")

  write_outputs \
    "$stable_release" \
    "$containers_present" \
    "$generate_sbom" \
    "$sign_artifacts" \
    "$create_release" \
    "$check_authorization" \
    "$run_version_bump" \
    "$create_draft_release"
}

main "$@"
