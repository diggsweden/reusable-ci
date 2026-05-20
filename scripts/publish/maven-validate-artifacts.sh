#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Validate that required Maven Central artifacts (sources + javadoc JARs) are present.
# Expects to run in the Maven project working directory.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  printf "Checking for required artifacts...\n"

  # Maven Central requires: main JAR, sources JAR, javadoc JAR, POM
  # Check in all target directories (supports multi-module projects)
  SOURCES_COUNT=$(find . -type f -name "*-sources.jar" -path "*/target/*" | wc -l)
  JAVADOC_COUNT=$(find . -type f -name "*-javadoc.jar" -path "*/target/*" | wc -l)

  if [[ "$SOURCES_COUNT" -eq 0 ]]; then
    ci_log_error "Missing sources JAR. Maven Central requires sources."
    ci_log_error "Build with build-type: lib to generate sources and javadoc JARs"
    exit 1
  fi

  if [[ "$JAVADOC_COUNT" -eq 0 ]]; then
    ci_log_error "Missing javadoc JAR. Maven Central requires javadoc."
    ci_log_error "Build with build-type: lib to generate sources and javadoc JARs"
    exit 1
  fi

  printf "✓ All required artifacts present:\n"
  printf "  - Sources JARs: %s\n" "$SOURCES_COUNT"
  printf "  - Javadoc JARs: %s\n" "$JAVADOC_COUNT"
  find . -type f -name "*.jar" -path "*/target/*" ! -name "original-*.jar" -exec ls -lh {} \;
}

main "$@"
