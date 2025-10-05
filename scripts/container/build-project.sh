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
  echo "Building Maven project..."
  mvn clean package -DskipTests -Dstyle.color=always
  echo "✓ Maven build completed"
  ;;

npm)
  echo "Building NPM project..."
  npm ci --prefer-offline --no-audit
  npm run build
  echo "✓ NPM build completed"
  ;;

gradle)
  echo "Building Gradle project..."
  ./gradlew clean build -x test
  echo "✓ Gradle build completed"
  ;;

*)
  echo "::error::Unsupported project type: $PROJECT_TYPE"
  echo "Supported types: maven, npm, gradle"
  exit 1
  ;;
esac
