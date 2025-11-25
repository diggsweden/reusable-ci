#!/bin/bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
#
# SPDX-License-Identifier: CC0-1.0

# Parse artifacts.yml configuration
# This script is called by release-orchestrator.yml
set -euo pipefail

if [ ! -f "$ARTIFACTS_CONFIG" ]; then
  printf "::error::File not found: %s\n" "$ARTIFACTS_CONFIG"
  exit 1
fi

ARTIFACTS=$(yq eval -o=json '.artifacts' "$ARTIFACTS_CONFIG")
CONTAINERS=$(yq eval -o=json '.containers // []' "$ARTIFACTS_CONFIG")

if [ "$ARTIFACTS" = "null" ] || [ "$ARTIFACTS" = "[]" ]; then
  printf "::error::No artifacts found in %s\n" "$ARTIFACTS_CONFIG"
  exit 1
fi

# Validate projectType for all artifacts
VALID_TYPES="maven npm gradle gradle-android xcode-ios python go rust"
for projectType in $(printf "%s" "$ARTIFACTS" | jq -r '.[] | .["project-type"]'); do
  if ! printf "%s" "$VALID_TYPES" | grep -qw "$projectType"; then
    printf "::error::Invalid projectType '%s'. Must be one of: %s\n" "$projectType" "$VALID_TYPES"
    exit 1
  fi
done

# Validate container dependencies
ARTIFACT_NAMES=$(printf "%s" "$ARTIFACTS" | jq -r '.[].name')
CONTAINER_COUNT=$(printf "%s" "$CONTAINERS" | jq 'length')

if [ "$CONTAINER_COUNT" -gt 0 ]; then
  for container in $(printf "%s" "$CONTAINERS" | jq -c '.[]'); do
    CONTAINER_NAME=$(printf "%s" "$container" | jq -r '.name')
    FROM_ARTIFACTS=$(printf "%s" "$container" | jq -r '(.from // []) | .[]')

    for artifact in $FROM_ARTIFACTS; do
      if ! printf "%s" "$ARTIFACT_NAMES" | grep -q "^${artifact}$"; then
        printf "::error::Container '%s' references unknown artifact '%s'\n" "$CONTAINER_NAME" "$artifact"
        exit 1
      fi
    done

    # Warn if container has no dependencies
    FROM_COUNT=$(printf "%s" "$container" | jq '(.from // []) | length')
    if [ "$FROM_COUNT" -eq 0 ]; then
      printf "::warning::Container '%s' has empty 'from' array. No artifacts will be downloaded for this container.\n" "$CONTAINER_NAME"
    fi
  done
fi

# Resolve artifact types for each container from their dependencies
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

# Output all artifacts
{
  printf "artifacts<<EOF\n"
  printf "%s\n" "$ARTIFACTS"
  printf "EOF\n"
} >>"$GITHUB_OUTPUT"

{
  printf "containers<<EOF\n"
  printf "%s\n" "$CONTAINERS_WITH_TYPES"
  printf "EOF\n"
} >>"$GITHUB_OUTPUT"

# Filter artifacts by project type and set has-* flags
for type in maven npm gradle gradle-android xcode-ios python go rust; do
  FILTERED=$(printf "%s" "$ARTIFACTS" | jq -c '[.[] | select(.["project-type"] == "'"${type}"'")]')
  COUNT=$(printf "%s" "$FILTERED" | jq 'length')

  # Normalize type name for output (gradle-android -> gradleandroid)
  TYPE_NAME=$(printf "%s" "$type" | tr -d '-')

  # Output filtered list
  {
    printf "%s-artifacts<<EOF\n" "${TYPE_NAME}"
    printf "%s\n" "$FILTERED"
    printf "EOF\n"
  } >>"$GITHUB_OUTPUT"

  # Output has-* flag
  if [ "$COUNT" -gt 0 ]; then
    printf "has-%s=true\n" "${TYPE_NAME}" >>"$GITHUB_OUTPUT"
  else
    printf "has-%s=false\n" "${TYPE_NAME}" >>"$GITHUB_OUTPUT"
  fi
done

# Validate Maven applications are not published to github-packages (libraries only)
MAVEN_APPS_TO_GITHUB=$(printf "%s" "$ARTIFACTS" | jq -r '.[] | select(.["project-type"] == "maven" and .["build-type"] == "application" and (.["publish-to"] // [] | contains(["github-packages"]))) | .name')
if [ -n "$MAVEN_APPS_TO_GITHUB" ]; then
  printf "::warning::Maven applications should not be published to GitHub Packages (use only for libraries). JARs will be attached to GitHub Release instead. Skipping GitHub Packages publish for: %s\n" "$MAVEN_APPS_TO_GITHUB"
fi

# Filter artifacts by publish target
for target in maven-central github-packages; do
  # For github-packages, exclude Maven applications
  if [ "$target" = "github-packages" ]; then
    FILTERED=$(printf "%s" "$ARTIFACTS" | jq -c '[.[] | select((.["publish-to"] // []) | contains(["'"${target}"'"])) | select(.["project-type"] != "maven" or .["build-type"] != "application")]')
  else
    FILTERED=$(printf "%s" "$ARTIFACTS" | jq -c '[.[] | select((.["publish-to"] // []) | contains(["'"${target}"'"]))]')
  fi
  COUNT=$(printf "%s" "$FILTERED" | jq 'length')

  # Output filtered list
  # Normalize target name for output variable (remove hyphens: maven-central -> mavencentral)
  TARGET_NAME=$(printf "%s" "$target" | tr -d '-')
  {
    printf "%s-artifacts<<EOF\n" "${TARGET_NAME}"
    printf "%s\n" "$FILTERED"
    printf "EOF\n"
  } >>"$GITHUB_OUTPUT"

  # Output has-* flag
  if [ "$COUNT" -gt 0 ]; then
    printf "has-%s=true\n" "${TARGET_NAME}" >>"$GITHUB_OUTPUT"
  else
    printf "has-%s=false\n" "${TARGET_NAME}" >>"$GITHUB_OUTPUT"
  fi
done

# Extract first artifact's type and build-type for legacy compatibility
FIRST_PROJECT_TYPE=$(printf "%s" "$ARTIFACTS" | jq -r '.[0]["project-type"]')
FIRST_BUILD_TYPE=$(printf "%s" "$ARTIFACTS" | jq -r '.[0]["build-type"] // "application"')
printf "first-project-type=%s\n" "$FIRST_PROJECT_TYPE" >>"$GITHUB_OUTPUT"
printf "first-build-type=%s\n" "$FIRST_BUILD_TYPE" >>"$GITHUB_OUTPUT"

# Determine if release should be draft (SNAPSHOT or non-release version tags)
TAG_NAME="$GITHUB_REF_NAME"
IS_DRAFT="false"

# Check if tag contains -SNAPSHOT or -snapshot (always draft)
if [[ "$TAG_NAME" == *"-SNAPSHOT"* ]] || [[ "$TAG_NAME" == *"-snapshot"* ]]; then
  IS_DRAFT="true"
  printf "::notice::Tag contains -SNAPSHOT, creating draft release\n"
# Check if tag is a release version semver: vX.Y.Z or vX.Y.Z-prerelease
elif ! printf "%s" "$TAG_NAME" | grep -qE '^v?[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9]+(\.[a-zA-Z0-9]+)*)?$'; then
  IS_DRAFT="true"
  printf "::notice::Tag '%s' is not a release version semver (vX.Y.Z), creating draft release\n" "$TAG_NAME"
fi

printf "is-draft-release=%s\n" "$IS_DRAFT" >>"$GITHUB_OUTPUT"

{
  printf "## Configuration\n"
  printf "%s" "$ARTIFACTS" | jq -r '.[] | "### \(.name)\n- **Type:** \(.["project-type"])\n- **Publish To:** \(.["publish-to"] // [] | join(", "))\n- **Directory:** \(.["working-directory"])\n"'
} >>"$GITHUB_STEP_SUMMARY"

if [ "$CONTAINER_COUNT" -gt 0 ]; then
  {
    printf "\n"
    printf "## Containers\n"
    printf "%s" "$CONTAINERS_WITH_TYPES" | jq -r '.[] | "### \(.name)\n- **From:** \((.from // []) | join(", "))\n- **Artifact Types:** \(.["artifact-types"] | join(", "))\n- **Containerfile:** \(.["container-file"])\n"'
  } >>"$GITHUB_STEP_SUMMARY"
fi
