#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Extract and verify an npm tarball from build artifacts.
# Expects to run in the directory containing the tarball.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

find_tarball() {
  find . -maxdepth 1 \( -name "*.tgz" -o -name "*.tar.gz" \) -type f -print -quit
}

main() {
  TARBALL=$(find_tarball)
  if [[ -n "$TARBALL" ]]; then
    printf "Extracting %s...\n" "$TARBALL"
    tar -xzf "$TARBALL" --strip-components=1
    rm "$TARBALL"
    printf "Extracted contents:\n"
    find dist/ -type f -ls 2>/dev/null || find build/ -type f -ls 2>/dev/null || true
    printf "\nVerifying dist/cli.js exists:\n"
    test -f dist/cli.js && printf "✓ dist/cli.js found\n" || printf "✗ dist/cli.js NOT found\n"
  else
    ci_log_error "No tarball found in artifacts"
    exit 1
  fi
}

main "$@"
