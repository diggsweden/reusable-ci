#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
#
# SPDX-License-Identifier: CC0-1.0

# Container SBOM Generator Wrapper
#
# Purpose: Handles SBOM generation for containers with artifact dependencies.
# Simplifies the multi-artifact SBOM logic from publish-container.yml.
#
# Usage: generate-container-sbom.sh ARTIFACT_TYPES VERSION PROJECT_NAME IMAGE SCRIPT_DIR
#
# Arguments:
#   ARTIFACT_TYPES - Comma-separated list of artifact types (e.g., "maven,npm") or empty
#   VERSION        - Version string for SBOM
#   PROJECT_NAME   - Project name for SBOM
#   IMAGE          - Container image reference (registry/org/repo@sha256:...)
#   SCRIPT_DIR     - Path to scripts/sbom directory
#
# Examples:
#   generate-container-sbom.sh "maven" "1.0.0" "my-app" "ghcr.io/org/app@sha256:..." "./scripts/sbom"
#   generate-container-sbom.sh "" "1.0.0" "my-app" "ghcr.io/org/app@sha256:..." "./scripts/sbom"
#
# Output: SBOM files in current directory matching pattern: *-container-sbom.{spdx,cyclonedx}.json

set -euo pipefail

ARTIFACT_TYPES="$1"
VERSION="$2"
PROJECT_NAME="$3"
IMAGE="$4"
SCRIPT_DIR="$5"

# Handle empty artifact-types (containers building from source with no dependencies)
if [[ -z "$ARTIFACT_TYPES" ]]; then
  printf "No artifact dependencies - generating SBOM from container image only\n"
  bash "$SCRIPT_DIR/generate-sbom.sh" \
    "container" \
    "containerimage" \
    "$VERSION" \
    "$PROJECT_NAME" \
    "." \
    "$IMAGE"
else
  # Generate SBOM for each artifact type in multi-artifact containers
  IFS=',' read -ra TYPES <<<"$ARTIFACT_TYPES"

  for ARTIFACT_TYPE in "${TYPES[@]}"; do
    printf "Generating SBOM for artifact type: %s\n" "$ARTIFACT_TYPE"
    bash "$SCRIPT_DIR/generate-sbom.sh" \
      "$ARTIFACT_TYPE" \
      "containerimage" \
      "$VERSION" \
      "$PROJECT_NAME" \
      "." \
      "$IMAGE"
  done
fi

printf "✓ Container SBOM generation completed\n"
