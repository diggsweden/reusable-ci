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

compute_effective_sboms() {
  # Intersect the release-level cap (`release_sboms`) with the per-pipeline
  # union coming from parse-artifacts-config.sh (`pipeline_sboms`). Both are
  # comma-list strings using the same CISA layer vocabulary (or the shortcuts
  # 'all' / 'none'); expand them to sets via expand-sboms.sh, intersect,
  # emit the canonical comma-list or "none".
  #
  # Empty intersection is a misconfiguration (release cap excludes every
  # layer the artefacts want) — warn loudly and return "none" so the
  # pipeline still completes.
  local release_sboms="$1"
  local pipeline_sboms="$2"
  local expander="$SCRIPT_DIR/../config/expand-sboms.sh"

  local release_set pipeline_set intersection
  release_set=$(bash "$expander" "$release_sboms")
  pipeline_set=$(bash "$expander" "$pipeline_sboms")

  intersection=$(
    jq -rn --argjson a "$release_set" --argjson b "$pipeline_set" '
      ["build","analyzed-artifact","analyzed-container"]
      | map(select(. as $l | ($a | index($l)) and ($b | index($l))))
      | if length == 0 then "none" else join(",") end
    '
  )

  if [[ "$intersection" == "none" && "$pipeline_sboms" != "none" && "$release_sboms" != "none" ]]; then
    local msg
    msg="sboms: release cap '$release_sboms' has no overlap with artefact config '$pipeline_sboms' — no SBOMs will be generated."
    # Send to stderr so the function's captured stdout stays clean (GitHub
    # Actions picks up `::warning::` annotations from stderr too).
    ci_log_warning "$msg" >&2
    # Surface the misconfig in the step summary too so reviewers see it without
    # scrolling the raw log.
    {
      printf "\n### ⚠️ SBOM misconfiguration\n"
      printf "%s\n" "$msg"
      printf "Set the release-level \`sboms\` input and the per-artefact \`sboms\` field so they share at least one CISA layer, or use \`sboms: none\` if intentional.\n"
    } >>"$(ci_summary_file)"
  fi

  printf "%s\n" "$intersection"
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
  local effective_sboms="$3"
  local sign_artifacts="$4"
  local create_release="$5"
  local check_authorization="$6"
  local run_version_bump="$7"
  local create_draft_release="$8"

  ci_output "should-make-latest" "$stable_release"
  ci_output "has-containers" "$containers_present"
  ci_output "effective-sboms" "$effective_sboms"
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
  readonly RELEASE_SBOMS="${RELEASE_SBOMS:-all}"
  readonly RELEASE_SIGN_ARTIFACTS="${RELEASE_SIGN_ARTIFACTS:-false}"
  readonly CHANGELOG_CREATOR="${CHANGELOG_CREATOR:-}"
  readonly CHANGELOG_SKIP_VERSION_BUMP="${CHANGELOG_SKIP_VERSION_BUMP:-false}"
  readonly CI_REF_NAME="${CI_REF_NAME:?CI_REF_NAME is required}"
  readonly PIPELINE_SBOMS="${PIPELINE_SBOMS:-none}"
  readonly FIRST_REQUIRE_AUTHORIZATION="${FIRST_REQUIRE_AUTHORIZATION:-false}"
  readonly CONTAINERS="${CONTAINERS:-[]}"

  local stable_release
  local containers_present
  local effective_sboms
  local sign_artifacts
  local create_release
  local check_authorization
  local run_version_bump
  local draft_release
  local create_draft_release

  stable_release=$(is_stable_release "$RELEASE_TYPE" "$CI_REF_NAME")
  containers_present=$(has_containers "$CONTAINERS")
  effective_sboms=$(compute_effective_sboms "$RELEASE_SBOMS" "$PIPELINE_SBOMS")
  sign_artifacts=$(should_sign_artifacts "$RELEASE_SIGN_ARTIFACTS")
  create_release=$(should_create_release "$RELEASE_PUBLISHER")
  check_authorization=$(should_check_authorization "$FIRST_REQUIRE_AUTHORIZATION" "$RELEASE_CHECK_AUTHORIZATION")
  run_version_bump=$(should_run_version_bump "$CHANGELOG_SKIP_VERSION_BUMP" "$CHANGELOG_CREATOR")
  draft_release=$(is_draft_release "$CI_REF_NAME")
  create_draft_release=$(should_create_draft_release "$RELEASE_DRAFT" "$draft_release")

  write_outputs \
    "$stable_release" \
    "$containers_present" \
    "$effective_sboms" \
    "$sign_artifacts" \
    "$create_release" \
    "$check_authorization" \
    "$run_version_bump" \
    "$create_draft_release"
}

main "$@"
