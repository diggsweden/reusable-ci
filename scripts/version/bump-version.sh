#!/bin/bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

PROJECT_TYPE="${1}"
VERSION="${2}"
WORKING_DIR="${3:-.}"
GRADLE_VERSION_FILE="${4:-gradle.properties}"

echo "Bumping version to $VERSION for $PROJECT_TYPE project in $WORKING_DIR"

case "$PROJECT_TYPE" in
maven)
  cd "$WORKING_DIR"
  echo "Updating Maven version to $VERSION"
  # shellcheck disable=SC2086  # Word splitting is intentional for MAVEN_CLI_OPTS
  mvn ${MAVEN_CLI_OPTS:-} versions:set -DnewVersion="$VERSION" -DgenerateBackupPoms=false -DprocessAllModules=true -DskipTests
  echo "✓ Maven version updated (including all sub-modules)"
  ;;

npm)
  cd "$WORKING_DIR"
  echo "Updating NPM version to $VERSION"
  npm version "$VERSION" --no-git-tag-version --allow-same-version
  echo "✓ NPM version updated"
  ;;

gradle | gradle-android)
  cd "$WORKING_DIR"
  echo "Version file: $GRADLE_VERSION_FILE"

  if [ ! -f "$GRADLE_VERSION_FILE" ]; then
    echo "::error::Gradle version file not found: $GRADLE_VERSION_FILE"
    exit 1
  fi

  if grep -q '^versionName=' "$GRADLE_VERSION_FILE"; then
    sed -i "s/^versionName=.*/versionName=$VERSION/" "$GRADLE_VERSION_FILE"
    echo "Updated versionName to $VERSION"
  else
    echo "versionName=$VERSION" >>"$GRADLE_VERSION_FILE"
    echo "Added versionName=$VERSION"
  fi

  if grep -q '^versionCode=' "$GRADLE_VERSION_FILE"; then
    CURRENT_CODE=$(grep '^versionCode=' "$GRADLE_VERSION_FILE" | cut -d'=' -f2 | tr -d ' ')
    NEW_CODE=$((CURRENT_CODE + 1))
    sed -i "s/^versionCode=.*/versionCode=$NEW_CODE/" "$GRADLE_VERSION_FILE"
    echo "Incremented versionCode: $CURRENT_CODE → $NEW_CODE"
  else
    echo "versionCode=1" >>"$GRADLE_VERSION_FILE"
    echo "Added versionCode=1"
  fi

  echo "✓ Gradle version updated"
  cat "$GRADLE_VERSION_FILE"
  ;;

xcode-ios)
  cd "$WORKING_DIR"
  echo "Updating Xcode version to $VERSION"

  # Verify agvtool can read the project
  if ! agvtool what-version &>/dev/null; then
    echo "::error::Project not configured for agvtool (Apple Generic Versioning)"
    echo "::error::Please configure your Xcode project:"
    echo "::error::  1. Open project in Xcode"
    echo "::error::  2. Select project in navigator"
    echo "::error::  3. Build Settings → Versioning System → 'Apple Generic'"
    echo "::error::  4. Build Settings → Current Project Version → Set to '1'"
    exit 1
  fi

  # Update version and build number
  agvtool new-marketing-version "$VERSION"
  agvtool next-version -all

  echo "✓ Xcode version updated to $VERSION"
  ;;

*)
  echo "::error::Unknown project type: $PROJECT_TYPE"
  exit 1
  ;;
esac
