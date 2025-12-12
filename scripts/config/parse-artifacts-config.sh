#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

# Parse artifacts.yml configuration
# This script is called by release-orchestrator.yml
set -euo pipefail

readonly VALID_PROJECT_TYPES="maven npm gradle gradle-android xcode-ios python go rust meta"
readonly SBOM_SUPPORTED_TYPES="maven npm gradle python go rust"
readonly PUBLISH_TARGETS="maven-central github-packages google-play"

die() {
  printf "::error::%s\n" "$1"
  exit 1
}

warn() {
  printf "::warning::%s\n" "$1"
}

notice() {
  printf "::notice::%s\n" "$1"
}

output() {
  printf "%s\n" "$1" >>"$GITHUB_OUTPUT"
}

output_multiline() {
  local name="$1"
  local value="$2"
  {
    printf "%s<<EOF\n" "$name"
    printf "%s\n" "$value"
    printf "EOF\n"
  } >>"$GITHUB_OUTPUT"
}

summary() {
  printf "%s\n" "$1" >>"$GITHUB_STEP_SUMMARY"
}

normalize_name() {
  printf "%s" "$1" | tr -d '-'
}

load_config() {
  [[ -f "$ARTIFACTS_CONFIG" ]] || die "File not found: $ARTIFACTS_CONFIG"

  ARTIFACTS=$(yq eval -o=json '.artifacts' "$ARTIFACTS_CONFIG")
  CONTAINERS=$(yq eval -o=json '.containers // []' "$ARTIFACTS_CONFIG")

  [[ "$ARTIFACTS" != "null" && "$ARTIFACTS" != "[]" ]] || die "No artifacts found in $ARTIFACTS_CONFIG"
}

validate_project_types() {
  local project_type
  for project_type in $(printf "%s" "$ARTIFACTS" | jq -r '.[] | .["project-type"]'); do
    printf "%s" "$VALID_PROJECT_TYPES" | grep -qw "$project_type" ||
      die "Invalid projectType '$project_type'. Must be one of: $VALID_PROJECT_TYPES"
  done
}

validate_containers() {
  local artifact_names container container_name from_artifacts artifact from_count

  [[ $(printf "%s" "$CONTAINERS" | jq 'length') -gt 0 ]] || return 0

  artifact_names=$(printf "%s" "$ARTIFACTS" | jq -r '.[].name')

  while IFS= read -r container; do
    container_name=$(printf "%s" "$container" | jq -r '.name')
    from_artifacts=$(printf "%s" "$container" | jq -r '(.from // []) | .[]')

    for artifact in $from_artifacts; do
      printf "%s" "$artifact_names" | grep -q "^${artifact}$" ||
        die "Container '$container_name' references unknown artifact '$artifact'"
    done

    from_count=$(printf "%s" "$container" | jq '(.from // []) | length')
    [[ "$from_count" -gt 0 ]] ||
      warn "Container '$container_name' has empty 'from' array. No artifacts will be downloaded for this container."
  done < <(printf "%s" "$CONTAINERS" | jq -c '.[]')
}

resolve_container_types() {
  CONTAINERS_WITH_TYPES=$(printf "%s" "$CONTAINERS" | jq -c --argjson artifacts "$ARTIFACTS" '
    map(
      . + {
        "artifact-types": [
          (.from // [])[] as $dep |
          ($artifacts[] | select(.name == $dep) | .["project-type"])
        ] | unique
      }
    )
  ')
}

output_artifacts_by_type() {
  local type filtered count type_name

  for type in $VALID_PROJECT_TYPES; do
    filtered=$(printf "%s" "$ARTIFACTS" | jq -c '[.[] | select(.["project-type"] == "'"${type}"'")]')
    count=$(printf "%s" "$filtered" | jq 'length')
    type_name=$(normalize_name "$type")

    output_multiline "${type_name}-artifacts" "$filtered"

    if [[ "$count" -gt 0 ]]; then
      output "has-${type_name}=true"
    else
      output "has-${type_name}=false"
    fi
  done
}

validate_maven_publish() {
  local maven_apps_to_github
  maven_apps_to_github=$(printf "%s" "$ARTIFACTS" | jq -r '
    .[] | select(
      .["project-type"] == "maven" and
      .["build-type"] == "application" and
      (.["publish-to"] // [] | contains(["github-packages"]))
    ) | .name
  ')

  [[ -z "$maven_apps_to_github" ]] ||
    warn "Maven applications should not be published to GitHub Packages (use only for libraries). Skipping for: $maven_apps_to_github"
}

output_artifacts_by_publish_target() {
  local target filtered count target_name

  for target in $PUBLISH_TARGETS; do
    if [[ "$target" = "github-packages" ]]; then
      filtered=$(printf "%s" "$ARTIFACTS" | jq -c '
        [.[] | select(
          (.["publish-to"] // []) | contains(["'"${target}"'"])
        ) | select(
          .["project-type"] != "maven" or .["build-type"] != "application"
        )]
      ')
    else
      filtered=$(printf "%s" "$ARTIFACTS" | jq -c '
        [.[] | select((.["publish-to"] // []) | contains(["'"${target}"'"]))]
      ')
    fi

    count=$(printf "%s" "$filtered" | jq 'length')
    target_name=$(normalize_name "$target")

    output_multiline "${target_name}-artifacts" "$filtered"

    if [[ "$count" -gt 0 ]]; then
      output "has-${target_name}=true"
    else
      output "has-${target_name}=false"
    fi
  done
}

output_first_artifact_info() {
  local first_project_type first_build_type first_require_auth

  first_project_type=$(printf "%s" "$ARTIFACTS" | jq -r '.[0]["project-type"]')
  first_build_type=$(printf "%s" "$ARTIFACTS" | jq -r '.[0]["build-type"] // "application"')
  first_require_auth=$(printf "%s" "$ARTIFACTS" | jq -r '.[0]["require-authorization"] // false')
  first_artifact_name=$(printf "%s" "$ARTIFACTS" | jq -r '.[0].name // ""')

  output "first-project-type=$first_project_type"
  output "first-build-type=$first_build_type"
  output "first-require-authorization=$first_require_auth"
  output "first-artifact-name=$first_artifact_name"
}

compute_sbom_settings() {
  local artifacts_with_sbom needs_sbom sbom_artifacts

  artifacts_with_sbom=$(printf "%s" "$ARTIFACTS" | jq -c --arg supported "$SBOM_SUPPORTED_TYPES" '
    map(
      . as $item |
      . + {
        "generate-sbom": (
          if $item["generate-sbom"] != null then
            $item["generate-sbom"]
          else
            ($supported | split(" ") | index($item["project-type"]) != null)
          end
        )
      }
    )
  ')

  needs_sbom=$(printf "%s" "$artifacts_with_sbom" | jq 'any(.["generate-sbom"] == true)')
  sbom_artifacts=$(printf "%s" "$artifacts_with_sbom" | jq -c '[.[] | select(.["generate-sbom"] == true)]')

  output "needs-sbom=$needs_sbom"
  output_multiline "sbom-artifacts" "$sbom_artifacts"
}

determine_draft_release() {
  local tag_name="$GITHUB_REF_NAME"
  local is_draft="false"

  if [[ "$tag_name" == *"-SNAPSHOT"* ]] || [[ "$tag_name" == *"-snapshot"* ]]; then
    is_draft="true"
    notice "Tag contains -SNAPSHOT, creating draft release"
  elif ! printf "%s" "$tag_name" | grep -qE '^v?[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9]+(\.[a-zA-Z0-9]+)*)?$'; then
    is_draft="true"
    notice "Tag '$tag_name' is not a release version semver (vX.Y.Z), creating draft release"
  fi

  output "is-draft-release=$is_draft"
}

generate_summary() {
  local container_count

  summary "## Configuration"
  printf "%s" "$ARTIFACTS" | jq -r '
    .[] | "### \(.name)\n- **Type:** \(.["project-type"])\n- **Publish To:** \(.["publish-to"] // [] | join(", "))\n- **Directory:** \(.["working-directory"])\n"
  ' >>"$GITHUB_STEP_SUMMARY"

  container_count=$(printf "%s" "$CONTAINERS" | jq 'length')
  if [[ "$container_count" -gt 0 ]]; then
    summary ""
    summary "## Containers"
    printf "%s" "$CONTAINERS_WITH_TYPES" | jq -r '
      .[] | "### \(.name)\n- **From:** \((.from // []) | join(", "))\n- **Artifact Types:** \(.["artifact-types"] | join(", "))\n- **Containerfile:** \(.["container-file"])\n"
    ' >>"$GITHUB_STEP_SUMMARY"
  fi
}

main() {
  load_config
  validate_project_types
  validate_containers
  resolve_container_types

  output_multiline "artifacts" "$ARTIFACTS"
  output_multiline "containers" "$CONTAINERS_WITH_TYPES"

  output_artifacts_by_type
  validate_maven_publish
  output_artifacts_by_publish_target
  output_first_artifact_info
  compute_sbom_settings
  determine_draft_release
  generate_summary
}

main
