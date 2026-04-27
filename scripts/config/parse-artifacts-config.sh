#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Parse artifacts.yml configuration
# This script is called by release-orchestrator.yml
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"
source "$SCRIPT_DIR/../ci/env.sh"

readonly VALID_PROJECT_TYPES="maven npm gradle gradle-android xcode-ios python go rust meta"
readonly SBOM_SUPPORTED_TYPES="maven npm gradle gradle-android python go rust"
readonly PUBLISH_TARGETS="maven-central github-packages google-play"

die() {
  ci_log_error "$1"
  exit 1
}

warn() {
  ci_log_warning "$1"
}

output() {
  ci_output "${1%%=*}" "${1#*=}"
}

output_multiline() {
  local name="$1"
  local value="$2"
  printf "%s\n" "$value" | ci_output_multiline "$name"
}

summary() {
  printf "%s\n" "$1" >>"$(ci_summary_file)"
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
  # Enrich each container with two derived fields:
  #   artifact-types                  - unique project-types of the source artefacts
  #   enable-analyzed-container-sbom  - true if any source artefact wants
  #                                     'analyzed-container' in its
  #                                     effective-sboms (matches the
  #                                     publish-container.yml input that
  #                                     consumes it).
  #
  # The SBOM derivation replaces the v2.x user-facing `enable-sbom: bool` field
  # on the container block: container scanning is now driven by the per-artefact
  # `sboms` declaration so there's a single source of truth.
  #
  # IMPORTANT: this assumes ARTIFACTS already has effective-sboms enriched, so
  # main() must call compute_sbom_settings before resolve_container_types.
  CONTAINERS_WITH_TYPES=$(printf "%s" "$CONTAINERS" | jq -c --argjson artifacts "$ARTIFACTS" '
    map(
      . + {
        "artifact-types": [
          (.from // [])[] as $dep |
          ($artifacts[] | select(.name == $dep) | .["project-type"])
        ] | unique,
        "enable-analyzed-container-sbom": (
          [(.from // [])[] as $dep |
            ($artifacts[] | select(.name == $dep) | (.["effective-sboms"] // []) | index("analyzed-container"))]
          | any(. != null)
        )
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

filter_by_project_type() {
  local artifacts="$1" project_type="$2"
  printf "%s" "$artifacts" | jq -c "[.[] | select(.\"project-type\" == \"${project_type}\")]"
}

output_sublist() {
  local prefix="$1" type_name="$2" artifacts="$3"
  output_multiline "${prefix}-${type_name}-artifacts" "$artifacts"
}

output_artifacts_by_publish_target() {
  local target filtered count target_name
  local gp_filtered='[]' mc_filtered='[]'

  for target in $PUBLISH_TARGETS; do
    if [[ "$target" = "github-packages" ]]; then
      filtered=$(printf "%s" "$ARTIFACTS" | jq -c '
        [.[] | select(
          (.["publish-to"] // []) | contains(["'"${target}"'"])
        ) | select(
          .["project-type"] != "maven" or .["build-type"] != "application"
        )]
      ')
      gp_filtered="$filtered"
    else
      filtered=$(printf "%s" "$ARTIFACTS" | jq -c '
        [.[] | select((.["publish-to"] // []) | contains(["'"${target}"'"]))]
      ')
      if [[ "$target" = "maven-central" ]]; then
        mc_filtered="$filtered"
      fi
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

  # Sublists by project type for routing to the correct publish workflow.
  # Uses pre-computed filtered lists from above rather than re-querying ARTIFACTS.
  local gp_maven gp_gradle
  gp_maven=$(printf "%s" "$gp_filtered" | jq -c '[.[] | select(.["project-type"] == "maven" or .["project-type"] == "npm")]')
  gp_gradle=$(filter_by_project_type "$gp_filtered" "gradle")

  output_sublist "githubpackages" "maven" "$gp_maven"
  output_sublist "githubpackages" "gradle" "$gp_gradle"

  output_sublist "mavencentral" "maven" "$(filter_by_project_type "$mc_filtered" "maven")"
  output_sublist "mavencentral" "gradle" "$(filter_by_project_type "$mc_filtered" "gradle")"
}

output_first_artifact_info() {
  local first_project_type first_build_type first_require_auth

  first_project_type=$(printf "%s" "$ARTIFACTS" | jq -r '.[0]["project-type"]')
  first_build_type=$(printf "%s" "$ARTIFACTS" | jq -r '.[0]["build-type"] // "application"')
  first_require_auth=$(printf "%s" "$ARTIFACTS" | jq -r '.[0]["require-authorization"] // false')
  local first_artifact_name
  first_artifact_name=$(printf "%s" "$ARTIFACTS" | jq -r '.[0].name // ""')

  output "first-project-type=$first_project_type"
  output "first-build-type=$first_build_type"
  output "first-require-authorization=$first_require_auth"
  output "first-artifact-name=$first_artifact_name"
}

compute_sbom_settings() {
  local expander artifacts_with_sboms pipeline_sboms

  expander="$SCRIPT_DIR/expand-sboms.sh"

  # Fill in per-artefact `sboms` default when unset:
  #   - supported project types default to `all`
  #   - everything else defaults to `none`
  # Then validate every sboms value and annotate each artefact with its
  # effective CISA layer list (via expand-sboms.sh).
  artifacts_with_sboms=$(printf "%s" "$ARTIFACTS" | jq -c --arg supported "$SBOM_SUPPORTED_TYPES" '
    map(
      . as $item |
      . + {
        "sboms": (
          if $item.sboms != null then
            $item.sboms
          elif ($supported | split(" ") | index($item["project-type"]) != null) then
            "all"
          else
            "none"
          end
        )
      }
    )
  ')

  local count
  count=$(printf "%s" "$artifacts_with_sboms" | jq 'length')
  local i item name sboms_value effective
  for ((i = 0; i < count; i++)); do
    item=$(printf "%s" "$artifacts_with_sboms" | jq -c ".[$i]")
    name=$(printf "%s" "$item" | jq -r '.name')
    sboms_value=$(printf "%s" "$item" | jq -r '.sboms')

    if ! effective=$(bash "$expander" "$sboms_value" 2>&1); then
      die "artefact '$name': $effective"
    fi

    artifacts_with_sboms=$(
      printf "%s" "$artifacts_with_sboms" |
        jq --argjson i "$i" --argjson eff "$effective" '.[$i] += {"effective-sboms": $eff}'
    )
  done

  # Pipeline union: which CISA layers does any artefact want produced?
  # Emitted as a comma-list string (or "none" when empty) so the string form
  # mirrors the user-facing `sboms` enum shape end-to-end. Emits layers in a
  # stable canonical order (build, analyzed-artifact, analyzed-container)
  # regardless of artefact iteration order.
  pipeline_sboms=$(
    printf "%s" "$artifacts_with_sboms" |
      jq -r '
        [.[]["effective-sboms"][]] as $union |
        ["build","analyzed-artifact","analyzed-container"]
        | map(select(. as $l | $union | index($l)))
        | if length == 0 then "none" else join(",") end
      '
  )

  # Update the top-level ARTIFACTS so downstream outputs carry the sboms/effective-sboms fields.
  ARTIFACTS="$artifacts_with_sboms"

  output "pipeline-sboms=$pipeline_sboms"
}

generate_summary() {
  local container_count

  summary "## Configuration"
  printf "%s" "$ARTIFACTS" | jq -r '
    .[] | "### \(.name)\n- **Type:** \(.["project-type"])\n- **Publish To:** \(.["publish-to"] // [] | join(", "))\n- **Directory:** \(.["working-directory"])\n"
  ' >>"$(ci_summary_file)"

  container_count=$(printf "%s" "$CONTAINERS" | jq 'length')
  if [[ "$container_count" -gt 0 ]]; then
    summary ""
    summary "## Containers"
    printf "%s" "$CONTAINERS_WITH_TYPES" | jq -r '
      .[] | "### \(.name)\n- **From:** \((.from // []) | join(", "))\n- **Artifact Types:** \(.["artifact-types"] | join(", "))\n- **Containerfile:** \(.["container-file"])\n"
    ' >>"$(ci_summary_file)"
  fi
}

main() {
  load_config
  validate_project_types
  validate_containers

  # Enrich ARTIFACTS with sboms/effective-sboms fields BEFORE downstream
  # output or filtering so consumers see the full per-artefact shape.
  # Containers depend on this (enable-analyzed-container-sbom is derived from
  # each source artefact's effective-sboms), so order matters.
  compute_sbom_settings
  resolve_container_types

  output_multiline "artifacts" "$ARTIFACTS"
  output_multiline "containers" "$CONTAINERS_WITH_TYPES"

  output_artifacts_by_type
  validate_maven_publish
  output_artifacts_by_publish_target
  output_first_artifact_info
  generate_summary
}

main "$@"
