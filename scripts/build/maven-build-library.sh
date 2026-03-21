#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Build a Maven library with sources and javadoc JARs.
#
# Required env:
#   MAVEN_CLI_OPTS   Maven CLI options (batch-mode, etc.)
# Optional env:
#   MAVEN_PROFILE    Maven profile to activate (e.g., central-release)
#   SKIP_TESTS       "true" to skip tests (default: "false")

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  local MAVEN_PROFILE="${MAVEN_PROFILE:-}"
  local SKIP_TESTS="${SKIP_TESTS:-false}"

  printf "Building Maven library with sources and javadoc...\n"

  # Determine profile argument
  PROFILE_ARG=""
  if [[ -n "$MAVEN_PROFILE" ]]; then
    PROFILE_ARG="-P${MAVEN_PROFILE}"
    printf "Using Maven profile: %s\n" "$MAVEN_PROFILE"
  fi

  # shellcheck disable=SC2086
  mvn $MAVEN_CLI_OPTS clean compile $PROFILE_ARG

  if [[ "$SKIP_TESTS" != "true" ]]; then
    printf "Running tests...\n"
    # shellcheck disable=SC2086
    mvn $MAVEN_CLI_OPTS test $PROFILE_ARG
  fi

  printf "Creating library package with sources and javadoc...\n"
  # Skip GPG signing during build (will be done during publish)
  # shellcheck disable=SC2086
  mvn $MAVEN_CLI_OPTS package -DskipTests="$SKIP_TESTS" $PROFILE_ARG -Dgpg.skip=true
}

main "$@"
