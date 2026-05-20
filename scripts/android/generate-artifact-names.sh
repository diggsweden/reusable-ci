#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

main() {
  readonly INCLUDE_DATE="${1:-true}"
  readonly PREFIX="${2:-}"
  readonly REPO_NAME="${3:?Usage: $0 <include-date> <prefix> <repo-name> [flavor] [artifact-name]}"
  readonly FLAVOR="${4:-}"
  readonly ARTIFACT_NAME_OVERRIDE="${5:-}"

  local DEBUG_NAME RELEASE_NAME AAB_NAME SBOM_NAME

  if [[ -n "$ARTIFACT_NAME_OVERRIDE" ]]; then
    # Override mode: callers (typically release-build-stage.yml) supply the
    # canonical name from artifacts.yml, so build uploads under exactly the
    # name release-publish-stage.yml expects to download. The AAB inherits
    # the bare identifier because artifacts.yml `name:` semantically refers
    # to the deliverable AAB; APK/SBOM variants get suffixes to avoid
    # upload-artifact name collisions.
    DEBUG_NAME="${ARTIFACT_NAME_OVERRIDE}-debug"
    RELEASE_NAME="${ARTIFACT_NAME_OVERRIDE}-release"
    AAB_NAME="${ARTIFACT_NAME_OVERRIDE}"
    SBOM_NAME="${ARTIFACT_NAME_OVERRIDE}-sbom"
  else
    local DATE_STAMP=""
    if [[ "$INCLUDE_DATE" == "true" ]]; then
      DATE_STAMP="$(date +'%Y-%m-%d') - "
    fi

    local PREFIX_FORMATTED=""
    if [[ -n "$PREFIX" ]]; then
      PREFIX_FORMATTED="${PREFIX} - "
    fi

    local FLAVOR_SUFFIX=""
    if [[ -n "$FLAVOR" ]]; then
      FLAVOR_SUFFIX=" - ${FLAVOR}"
    fi

    DEBUG_NAME="${DATE_STAMP}${PREFIX_FORMATTED}${REPO_NAME}${FLAVOR_SUFFIX} - APK debug"
    RELEASE_NAME="${DATE_STAMP}${PREFIX_FORMATTED}${REPO_NAME}${FLAVOR_SUFFIX} - APK release"
    AAB_NAME="${DATE_STAMP}${PREFIX_FORMATTED}${REPO_NAME}${FLAVOR_SUFFIX} - AAB release"
    SBOM_NAME="${DATE_STAMP}${PREFIX_FORMATTED}${REPO_NAME}${FLAVOR_SUFFIX} - build SBOM"
  fi

  printf "debug-name=%s\n" "${DEBUG_NAME}"
  printf "release-name=%s\n" "${RELEASE_NAME}"
  printf "aab-name=%s\n" "${AAB_NAME}"
  printf "sbom-name=%s\n" "${SBOM_NAME}"

  printf "Debug artifact: %s\n" "${DEBUG_NAME}" >&2
  printf "Release artifact: %s\n" "${RELEASE_NAME}" >&2
  printf "AAB artifact: %s\n" "${AAB_NAME}" >&2
  printf "SBOM artifact: %s\n" "${SBOM_NAME}" >&2
}

main "$@"
