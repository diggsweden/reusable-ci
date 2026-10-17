#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Parse artifacts.yml configuration
# This script is called by release-orchestrator.yml
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"
source "$SCRIPT_DIR/../ci/env.sh"

readonly VALID_PROJECT_TYPES="maven npm gradle gradle-android xcode-ios python go cargo meta"
readonly SBOM_SUPPORTED_TYPES="maven npm gradle gradle-android python go cargo"
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
        ),
        # docker/build-push-action wants build-args as KEY=VALUE lines
        # (multi-line string). artifacts.yml uses an object map for ergonomics;
        # convert here so release-publish-stage can pass the string form
        # straight through. Empty when no build-args declared.
        "build-args-string": (
          (."build-args" // {})
          | to_entries
          | map("\(.key)=\(.value)")
          | join("\n")
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

# Shared jq definitions for the publish routing below.
#   maven_target: a target served by Maven repository publishers.
#   jvm_app: a Maven or Gradle application; applications are not published
#     to GitHub Packages, which is for libraries.
readonly JQ_PUBLISH_DEFS='
  def maven_target: . == "github-packages" or . == "maven-central";
  def maven_targets: [(.["publish-to"] // [])[] | select(maven_target)];
  def jvm_app: .["build-type"] == "application" and (.["project-type"] | IN("maven", "gradle"));
'

validate_maven_publish() {
  local apps_to_github
  apps_to_github=$(printf "%s" "$ARTIFACTS" | jq -r "$JQ_PUBLISH_DEFS"'
    .[] | select(jvm_app and (.["publish-to"] // [] | index("github-packages"))) | .name
  ')

  [[ -z "$apps_to_github" ]] ||
    warn "Applications should not be published to GitHub Packages (use only for libraries). Skipping for: $apps_to_github"

  local android_to_maven
  android_to_maven=$(printf "%s" "$ARTIFACTS" | jq -r "$JQ_PUBLISH_DEFS"'
    .[] | select(.["project-type"] == "gradle-android" and (maven_targets | length > 0)) | .name
  ')

  [[ -z "$android_to_maven" ]] ||
    warn "gradle-android artifacts cannot be published to GitHub Packages or Maven Central yet. Skipping for: $android_to_maven"
}

# config.publish-tasks is a string or a map from Maven target to a string.
# A string with both targets would run the same task in both jobs, and a map
# key that is misspelled or not in publish-to would silently fall back to the
# default publishing.
validate_gradle_publish_tasks() {
  local invalid
  invalid=$(printf "%s" "$ARTIFACTS" | jq -r "$JQ_PUBLISH_DEFS"'
    .[] | select(.["project-type"] == "gradle") | (.config["publish-tasks"] // "") as $tasks
    | maven_targets as $targets
    | select(
        (($tasks | type) == "string" and $tasks != "" and ($targets | length > 1)) or
        (($tasks | type) == "object" and
          ($tasks | to_entries | any((.key as $key | $targets | index($key) | not) or (.value | type) != "string"))) or
        (($tasks | type) | IN("string", "object") | not)
      ) | .name
  ')

  [[ -z "$invalid" ]] ||
    die "config.publish-tasks must be a string for one target, or map targets from publish-to (github-packages, maven-central) to a string: $invalid"

  local publishing_build
  publishing_build=$(printf "%s" "$ARTIFACTS" | jq -r "$JQ_PUBLISH_DEFS"'
    .[] | select(
      .["project-type"] == "gradle" and (maven_targets | length > 0) and
      ((.config["gradle-tasks"] // "") | test("(^|[\\s:])publish(\\s|$|\\S*Repository|\\S*MavenCentral)"))
    ) | .name
  ')

  [[ -z "$publishing_build" ]] ||
    warn "config.gradle-tasks publishes during the build, and the publish stage publishes again. Remove the publish task from gradle-tasks for: $publishing_build"
}

# Gradle and gradle-android artifacts never reach the Maven-based
# github-packages and maven-central publishers: Gradle publishes from source
# through publish-gradle.yml, and gradle-android has no Maven path yet.
output_artifacts_by_publish_target() {
  local target filtered count target_name

  for target in $PUBLISH_TARGETS; do
    filtered=$(printf "%s" "$ARTIFACTS" | jq -c --arg target "$target" "$JQ_PUBLISH_DEFS"'
      [.[] | select((.["publish-to"] // []) | index($target))
        | select(($target | maven_target | not) or (.["project-type"] | IN("gradle", "gradle-android") | not))
        | select($target != "github-packages" or (jvm_app | not))]
    ')

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

# One entry per Gradle artifact and Maven target, so each target publishes
# in its own job and a failed one can be re-run alone. Each entry carries
# publish-target and the publish-tasks override resolved for that target.
output_gradle_publish_artifacts() {
  local filtered count

  filtered=$(printf "%s" "$ARTIFACTS" | jq -c "$JQ_PUBLISH_DEFS"'
    [.[] | select(.["project-type"] == "gradle") | . as $artifact
      | maven_targets[]
      | select(. != "github-packages" or ($artifact | jvm_app | not)) | . as $target
      | ($artifact.config["publish-tasks"] // "") as $override
      | $artifact + {
          "publish-target": $target,
          "publish-tasks": (if ($override | type) == "object" then ($override[$target] // "") else $override end)
        }]
  ')
  count=$(printf "%s" "$filtered" | jq 'length')

  output_multiline "gradlepublish-artifacts" "$filtered"

  if [[ "$count" -gt 0 ]]; then
    output "has-gradlepublish=true"
  else
    output "has-gradlepublish=false"
  fi
}

# Any Maven or Gradle artifact publishing to Maven Central. A Central release
# is permanent, so the release plan then requires authorized releasers.
output_publishes_maven_central() {
  local publishes
  publishes=$(printf "%s" "$ARTIFACTS" | jq -r '
    any(.[]; (.["project-type"] | IN("maven", "gradle")) and
      ((.["publish-to"] // []) | index("maven-central")))
  ')
  output "publishes-maven-central=$publishes"
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
  validate_gradle_publish_tasks
  output_artifacts_by_publish_target
  output_gradle_publish_artifacts
  output_publishes_maven_central
  output_first_artifact_info
  generate_summary
}

main "$@"
