#!/bin/bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
#
# SPDX-License-Identifier: CC0-1.0

# Parse artifacts.yml configuration
# This script is called by release-orchestrator.yml
set -e

if [ ! -f "$ARTIFACTS_CONFIG" ]; then
  echo "::error::File not found: $ARTIFACTS_CONFIG"
  exit 1
fi

ARTIFACTS=$(yq eval -o=json '.artifacts' "$ARTIFACTS_CONFIG")
CONTAINERS=$(yq eval -o=json '.containers // []' "$ARTIFACTS_CONFIG")

if [ "$ARTIFACTS" = "null" ] || [ "$ARTIFACTS" = "[]" ]; then
  echo "::error::No artifacts found in $ARTIFACTS_CONFIG"
  exit 1
fi

# Validate projectType for all artifacts
VALID_TYPES="maven npm gradle python go rust"
for projectType in $(echo "$ARTIFACTS" | jq -r '.[] | .["project-type"]'); do
  if ! echo "$VALID_TYPES" | grep -qw "$projectType"; then
    echo "::error::Invalid projectType '$projectType'. Must be one of: $VALID_TYPES"
    exit 1
  fi
done

# Validate container dependencies
ARTIFACT_NAMES=$(echo "$ARTIFACTS" | jq -r '.[].name')
CONTAINER_COUNT=$(echo "$CONTAINERS" | jq 'length')

if [ "$CONTAINER_COUNT" -gt 0 ]; then
  for container in $(echo "$CONTAINERS" | jq -c '.[]'); do
    CONTAINER_NAME=$(echo "$container" | jq -r '.name')
    FROM_ARTIFACTS=$(echo "$container" | jq -r '(.from // []) | .[]')
    
    for artifact in $FROM_ARTIFACTS; do
      if ! echo "$ARTIFACT_NAMES" | grep -q "^${artifact}$"; then
        echo "::error::Container '$CONTAINER_NAME' references unknown artifact '$artifact'"
        exit 1
      fi
    done
    
    # Warn if container has no dependencies
    FROM_COUNT=$(echo "$container" | jq '(.from // []) | length')
    if [ "$FROM_COUNT" -eq 0 ]; then
      echo "::warning::Container '$CONTAINER_NAME' has empty 'from' array. No artifacts will be downloaded for this container."
    fi
  done
fi

# Resolve artifact types for each container from their dependencies
CONTAINERS_WITH_TYPES=$(echo "$CONTAINERS" | jq -c --argjson artifacts "$ARTIFACTS" '
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
  echo "artifacts<<EOF"
  echo "$ARTIFACTS"
  echo "EOF"
} >> $GITHUB_OUTPUT

{
  echo "containers<<EOF"
  echo "$CONTAINERS_WITH_TYPES"
  echo "EOF"
} >> $GITHUB_OUTPUT

# Filter artifacts by project type and set has-* flags
for type in maven npm gradle python go rust; do
  FILTERED=$(echo "$ARTIFACTS" | jq -c '[.[] | select(.["project-type"] == "'"${type}"'")]')
  COUNT=$(echo "$FILTERED" | jq 'length')
  
  # Output filtered list
  {
    echo "${type}-artifacts<<EOF"
    echo "$FILTERED"
    echo "EOF"
  } >> $GITHUB_OUTPUT
  
  # Output has-* flag
  if [ "$COUNT" -gt 0 ]; then
    echo "has-${type}=true" >> $GITHUB_OUTPUT
  else
    echo "has-${type}=false" >> $GITHUB_OUTPUT
  fi
done

# Validate Maven applications are not published to github-packages (libraries only)
MAVEN_APPS_TO_GITHUB=$(echo "$ARTIFACTS" | jq -r '.[] | select(.["project-type"] == "maven" and .["build-type"] == "application" and (.["publish-to"] // [] | contains(["github-packages"]))) | .name')
if [ -n "$MAVEN_APPS_TO_GITHUB" ]; then
  echo "::warning::Maven applications should not be published to GitHub Packages (use only for libraries). JARs will be attached to GitHub Release instead. Skipping GitHub Packages publish for: $MAVEN_APPS_TO_GITHUB"
fi

# Filter artifacts by publish target
for target in maven-central github-packages; do
  # For github-packages, exclude Maven applications
  if [ "$target" = "github-packages" ]; then
    FILTERED=$(echo "$ARTIFACTS" | jq -c '[.[] | select((.["publish-to"] // []) | contains(["'"${target}"'"])) | select(.["project-type"] != "maven" or .["build-type"] != "application")]')
  else
    FILTERED=$(echo "$ARTIFACTS" | jq -c '[.[] | select((.["publish-to"] // []) | contains(["'"${target}"'"]))]')
  fi
  COUNT=$(echo "$FILTERED" | jq 'length')
  
  # Output filtered list
  # Normalize target name for output variable (remove hyphens: maven-central -> mavencentral)
  TARGET_NAME=$(echo "$target" | tr -d '-')
  {
    echo "${TARGET_NAME}-artifacts<<EOF"
    echo "$FILTERED"
    echo "EOF"
  } >> $GITHUB_OUTPUT
  
  # Output has-* flag
  if [ "$COUNT" -gt 0 ]; then
    echo "has-${TARGET_NAME}=true" >> $GITHUB_OUTPUT
  else
    echo "has-${TARGET_NAME}=false" >> $GITHUB_OUTPUT
  fi
done

ARTIFACT_COUNT=$(echo "$ARTIFACTS" | jq 'length')
echo "::notice::Loaded $ARTIFACT_COUNT artifacts, $CONTAINER_COUNT containers"

# Extract first artifact's type and build-type for legacy compatibility
FIRST_PROJECT_TYPE=$(echo "$ARTIFACTS" | jq -r '.[0]["project-type"]')
FIRST_BUILD_TYPE=$(echo "$ARTIFACTS" | jq -r '.[0]["build-type"] // "application"')
echo "first-project-type=$FIRST_PROJECT_TYPE" >> $GITHUB_OUTPUT
echo "first-build-type=$FIRST_BUILD_TYPE" >> $GITHUB_OUTPUT

# Determine if release should be draft (SNAPSHOT or non-release version tags)
TAG_NAME="$GITHUB_REF_NAME"
IS_DRAFT="false"

# Check if tag contains -SNAPSHOT (always draft)
if [[ "$TAG_NAME" == *"-SNAPSHOT"* ]]; then
  IS_DRAFT="true"
  echo "::notice::Tag contains -SNAPSHOT, creating draft release"
# Check if tag is a release version semver: vX.Y.Z or vX.Y.Z-prerelease
elif ! echo "$TAG_NAME" | grep -qE '^v?[0-9]+\\.[0-9]+\\.[0-9]+(-[a-zA-Z0-9]+(\\.[a-zA-Z0-9]+)*)?$'; then
  IS_DRAFT="true"
  echo "::notice::Tag '$TAG_NAME' is not a release version semver (vX.Y.Z), creating draft release"
fi

echo "is-draft-release=$IS_DRAFT" >> $GITHUB_OUTPUT

echo "## Configuration" >> $GITHUB_STEP_SUMMARY
echo "$ARTIFACTS" | jq -r '.[] | "### \(.name)\n- **Type:** \(.["project-type"])\n- **Publish To:** \(.["publish-to"] // [] | join(", "))\n- **Directory:** \(.["working-directory"])\n"' >> $GITHUB_STEP_SUMMARY

if [ "$CONTAINER_COUNT" -gt 0 ]; then
  echo "" >> $GITHUB_STEP_SUMMARY
  echo "## Containers" >> $GITHUB_STEP_SUMMARY
  echo "$CONTAINERS_WITH_TYPES" | jq -r '.[] | "### \(.name)\n- **From:** \((.from // []) | join(", "))\n- **Artifact Types:** \(.["artifact-types"] | join(", "))\n- **Containerfile:** \(.["container-file"])\n"' >> $GITHUB_STEP_SUMMARY
fi
