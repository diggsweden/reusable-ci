#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

main() {
  readonly PROJECT_TYPE="${1:?Usage: $0 <project-type>}"

  case "$PROJECT_TYPE" in
  maven)
    printf "name=maven-build-artifacts\n"
    ;;
  npm)
    printf "name=npm-build-artifacts\n"
    ;;
  gradle)
    printf "name=gradle-build-artifacts\n"
    ;;
  python)
    printf "name=python-build-artifacts\n"
    ;;
  go)
    printf "name=go-build-artifacts\n"
    ;;
  rust)
    # Rust workflow is SBOM-only and not wired into release-orchestrator;
    # this name stays in sync with build-rust.yml for consumers that
    # call it directly.
    printf "name=rust-build-sbom\n"
    ;;
  *)
    printf "name=build-artifacts\n"
    ;;
  esac
}

main "$@"
