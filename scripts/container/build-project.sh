#!/bin/bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
#
# SPDX-License-Identifier: CC0-1.0

# Project Builder for Containers
#
# Purpose: Handles project builds for container workflows (Maven/NPM/Gradle).
# Extracts duplicated build logic from publish-container-dev.yml.
#
# Usage: build-project.sh PROJECT_TYPE WORKING_DIR
#
# Arguments:
#   PROJECT_TYPE   - Type of project: maven, npm, gradle
#   WORKING_DIR    - Working directory for the build
#
# Examples:
#   build-project.sh maven .
#   build-project.sh npm .
#
# Note: Node.js/Java setup must be done in the workflow before calling this script.
#
# Exit codes:
#   0 - Build successful
#   1 - Build failed or invalid project type

set -euo pipefail

PROJECT_TYPE="$1"
WORKING_DIR="${2:-.}"

cd "$WORKING_DIR" || exit 1

case "$PROJECT_TYPE" in
maven)
  printf "Building Maven project...\n"
  mvn clean package -DskipTests -Dstyle.color=always
  printf "✓ Maven build completed\n"
  ;;

npm)
  printf "Building NPM project...\n"
  npm ci --prefer-offline --no-audit
  npm run build
  printf "✓ NPM build completed\n"
  ;;

gradle)
  printf "Building Gradle project...\n"
  ./gradlew clean build -x test
  printf "✓ Gradle build completed\n"
  ;;

*)
  printf "::error::Unsupported project type: %s\n" "$PROJECT_TYPE"
  printf "Supported types: maven, npm, gradle\n"
  exit 1
  ;;
esac
