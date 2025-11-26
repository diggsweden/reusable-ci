#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
# SPDX-License-Identifier: CC0-1.0

# Create SBOM ZIP archive containing all 3 layers
# Usage: create-sbom-zip.sh <project-name> <version>

set -uo pipefail

PROJECT_NAME="${1:-$(basename "$(git config --get remote.origin.url)" .git)}"
VERSION="${2:-unknown}"
VERSION="${VERSION#v}" # Remove 'v' prefix if present

# Count SBOMs to determine if we need to create a ZIP
SBOM_COUNT=$(find . -maxdepth 1 -name '*-sbom.*.json' 2>/dev/null | wc -l)
[ -d "./sbom-artifacts" ] && SBOM_COUNT=$((SBOM_COUNT + $(find ./sbom-artifacts -name '*-container-sbom.*.json' 2>/dev/null | wc -l)))

if [ "$SBOM_COUNT" -eq 0 ]; then
  printf "No SBOMs found, skipping ZIP creation\n"
  exit 0
fi

printf "Creating SBOM zip archive with all 3 layers\n"
SBOM_ZIP="${PROJECT_NAME}-${VERSION}-sboms.zip"

# Add all three layers to ZIP
# Layer 1: Source SBOMs (pom for Maven, package for NPM, gradle for Gradle)
# Layer 2: Artifact SBOMs (jar for Maven/Gradle, tararchive for NPM)
for sbom in *-pom-sbom.spdx.json *-pom-sbom.cyclonedx.json \
  *-package-sbom.spdx.json *-package-sbom.cyclonedx.json \
  *-gradle-sbom.spdx.json *-gradle-sbom.cyclonedx.json \
  *-jar-sbom.spdx.json *-jar-sbom.cyclonedx.json \
  *-tararchive-sbom.spdx.json *-tararchive-sbom.cyclonedx.json; do
  if [ -f "$sbom" ]; then
    zip "$SBOM_ZIP" "$sbom"
    printf "  Added: %s\n" "$sbom"
  fi
done

# Add container SBOMs to zip if they exist (Layer 3)
if [ -d "./sbom-artifacts" ]; then
  for sbom in ./sbom-artifacts/*-container-sbom.*.json; do
    if [ -f "$sbom" ]; then
      zip -j "$SBOM_ZIP" "$sbom"
      printf "  Added: %s\n" "$(basename "$sbom")"
    fi
  done
fi

printf "SBOM ZIP contents:\n"
unzip -l "$SBOM_ZIP"

printf "Created SBOM ZIP: %s\n" "$SBOM_ZIP"
