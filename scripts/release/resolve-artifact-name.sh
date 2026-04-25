#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
#
# Resolve the uploaded artifact name(s) for a given project type. Emits
# GitHub Actions outputs on stdout:
#   name=<build-artifact-name>
#   sbom-name=<sbom-artifact-name>
#
# For gradle, ARTIFACT_NAME may be set to honour the `artifact-name` input
# that build-gradle-app.yml passes through when a matrix dispatch uses a
# user-defined name from artifacts.yml. Other stacks have a fixed upload
# name so the env var is ignored there.
set -euo pipefail

main() {
  readonly PROJECT_TYPE="${1:?Usage: $0 <project-type>}"

  case "$PROJECT_TYPE" in
  maven)
    printf "name=maven-build-artifacts\n"
    printf "sbom-name=maven-build-sbom\n"
    ;;
  npm)
    printf "name=npm-build-artifacts\n"
    printf "sbom-name=npm-build-sbom\n"
    ;;
  gradle)
    # Pair SBOM name with build artifact name so matrix dispatch doesn't
    # collide. Mirrors the logic in build-gradle-app.yml.
    if [[ -n "${ARTIFACT_NAME:-}" ]]; then
      printf "name=%s\n" "$ARTIFACT_NAME"
      printf "sbom-name=%s-sbom\n" "$ARTIFACT_NAME"
    else
      printf "name=gradle-build-artifacts\n"
      printf "sbom-name=gradle-build-sbom\n"
    fi
    ;;
  python)
    printf "name=python-build-artifacts\n"
    printf "sbom-name=python-build-sbom\n"
    ;;
  go)
    printf "name=go-build-artifacts\n"
    printf "sbom-name=go-build-sbom\n"
    ;;
  rust)
    # Rust workflow is SBOM-only and not wired into release-orchestrator;
    # this name stays in sync with build-rust.yml for consumers that
    # call it directly. `name` and `sbom-name` are identical because the
    # workflow only produces an SBOM artifact.
    printf "name=rust-build-sbom\n"
    printf "sbom-name=rust-build-sbom\n"
    ;;
  *)
    printf "name=build-artifacts\n"
    printf "sbom-name=build-sbom\n"
    ;;
  esac
}

main "$@"
