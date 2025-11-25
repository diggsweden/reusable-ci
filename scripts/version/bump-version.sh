#!/bin/bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

PROJECT_TYPE="${1}"
VERSION="${2}"
WORKING_DIR="${3:-.}"
GRADLE_VERSION_FILE="${4:-gradle.properties}"

printf "Bumping version to %s for %s project in %s\n" "$VERSION" "$PROJECT_TYPE" "$WORKING_DIR"

case "$PROJECT_TYPE" in
maven)
  cd "$WORKING_DIR"
  printf "Updating Maven version to %s\n" "$VERSION"
  # shellcheck disable=SC2086  # Word splitting is intentional for MAVEN_CLI_OPTS
  mvn ${MAVEN_CLI_OPTS:-} versions:set -DnewVersion="$VERSION" -DgenerateBackupPoms=false -DprocessAllModules=true -DskipTests
  printf "✓ Maven version updated (including all sub-modules)\n"
  ;;

npm)
  cd "$WORKING_DIR"
  printf "Updating NPM version to %s\n" "$VERSION"
  npm version "$VERSION" --no-git-tag-version --allow-same-version
  printf "✓ NPM version updated\n"
  ;;

gradle | gradle-android)
  cd "$WORKING_DIR"
  printf "Version file: %s\n" "$GRADLE_VERSION_FILE"

  if [ ! -f "$GRADLE_VERSION_FILE" ]; then
    printf "::error::Gradle version file not found: %s\n" "$GRADLE_VERSION_FILE"
    exit 1
  fi

  if grep -q '^versionName=' "$GRADLE_VERSION_FILE"; then
    sed -i "s/^versionName=.*/versionName=$VERSION/" "$GRADLE_VERSION_FILE"
    printf "Updated versionName to %s\n" "$VERSION"
  else
    printf "versionName=%s\n" "$VERSION" >>"$GRADLE_VERSION_FILE"
    printf "Added versionName=%s\n" "$VERSION"
  fi

  if grep -q '^versionCode=' "$GRADLE_VERSION_FILE"; then
    CURRENT_CODE=$(grep '^versionCode=' "$GRADLE_VERSION_FILE" | cut -d'=' -f2 | tr -d ' ')
    NEW_CODE=$((CURRENT_CODE + 1))
    sed -i "s/^versionCode=.*/versionCode=$NEW_CODE/" "$GRADLE_VERSION_FILE"
    printf "Incremented versionCode: %s → %s\n" "$CURRENT_CODE" "$NEW_CODE"
  else
    printf "versionCode=1\n" >>"$GRADLE_VERSION_FILE"
    printf "Added versionCode=1\n"
  fi

  printf "✓ Gradle version updated\n"
  cat "$GRADLE_VERSION_FILE"
  ;;

xcode-ios)
  cd "$WORKING_DIR"
  printf "Updating Xcode version to %s\n" "$VERSION"

  # Verify agvtool can read the project
  if ! agvtool what-version &>/dev/null; then
    printf "::error::Project not configured for agvtool (Apple Generic Versioning)\n"
    printf "::error::Please configure your Xcode project:\n"
    printf "::error::  1. Open project in Xcode\n"
    printf "::error::  2. Select project in navigator\n"
    printf "::error::  3. Build Settings → Versioning System → 'Apple Generic'\n"
    printf "::error::  4. Build Settings → Current Project Version → Set to '1'\n"
    exit 1
  fi

  # Update version and build number
  agvtool new-marketing-version "$VERSION"
  agvtool next-version -all

  printf "✓ Xcode version updated to %s\n" "$VERSION"
  ;;

*)
  printf "::error::Unknown project type: %s\n" "$PROJECT_TYPE"
  exit 1
  ;;
esac
