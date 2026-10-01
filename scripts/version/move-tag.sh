#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

# The base commit and tag object must come from validation before version bumping.
# Usage: move-tag.sh <tag-name> <release-base-sha> <release-tag-object>
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  local TAG_NAME="${1:-}"
  local BASE_SHA="${2:-}"
  local EXPECTED_TAG_OBJECT="${3:-}"
  local TAG_REF="refs/tags/$TAG_NAME"
  local TAG_OBJECT TAG_SHA RELEASE_SHA REMOTE_TAG_OBJECT

  if [[ $# -ne 3 || -z "$TAG_NAME" || -z "$BASE_SHA" || -z "$EXPECTED_TAG_OBJECT" ]]; then
    ci_log_error "Usage: move-tag.sh <tag-name> <release-base-sha> <release-tag-object>"
    return 1
  fi
  if ! git check-ref-format "$TAG_REF"; then
    ci_log_error "Invalid release tag name: $TAG_NAME"
    return 1
  fi

  if ! BASE_SHA=$(git rev-parse --verify --end-of-options "$BASE_SHA^{commit}" 2>/dev/null) ||
    ! EXPECTED_TAG_OBJECT=$(git rev-parse --verify --end-of-options "$EXPECTED_TAG_OBJECT^{object}" 2>/dev/null); then
    ci_log_error "The recorded release base or tag object is missing from the checkout"
    return 1
  fi
  if ! TAG_OBJECT=$(git rev-parse --verify "$TAG_REF" 2>/dev/null) ||
    ! TAG_SHA=$(git rev-parse --verify "$TAG_REF^{commit}" 2>/dev/null); then
    ci_log_error "Release tag '$TAG_NAME' is missing or does not point to a commit"
    return 1
  fi
  RELEASE_SHA=$(git rev-parse --verify HEAD)

  if [[ "$TAG_OBJECT" != "$EXPECTED_TAG_OBJECT" || "$TAG_SHA" != "$BASE_SHA" ]]; then
    ci_log_error "Release tag '$TAG_NAME' changed since validation"
    printf "Expected tag object: %s\nFound: %s\n" "$EXPECTED_TAG_OBJECT" "$TAG_OBJECT"
    printf "Expected tag commit: %s\nFound: %s\n" "$BASE_SHA" "$TAG_SHA"
    return 1
  fi

  if [[ "$RELEASE_SHA" == "$BASE_SHA" ]]; then
    # No version/changelog commit was created. Preserve the original signed tag.
    REMOTE_TAG_OBJECT=$(git ls-remote --exit-code --refs origin "$TAG_REF") || return 1
    REMOTE_TAG_OBJECT="${REMOTE_TAG_OBJECT%%[[:space:]]*}"
    if [[ "$REMOTE_TAG_OBJECT" != "$EXPECTED_TAG_OBJECT" ]]; then
      ci_log_error "Remote release tag '$TAG_NAME' changed since validation"
      return 1
    fi
    printf "Tag %s already points to release commit %s; no tag update needed\n" "$TAG_NAME" "$RELEASE_SHA"
    ci_output "release-sha" "$RELEASE_SHA"
    return 0
  fi

  # Only the single version-bump commit may follow the captured base. This also
  # rejects merges, unrelated commits and incomplete parent information.
  if [[ "$(git rev-list --parents -n 1 HEAD)" != "$RELEASE_SHA $BASE_SHA" ]]; then
    ci_log_error "Release commit must be a single commit directly after the validated base"
    printf "Expected base: %s\nRelease HEAD: %s\n" "$BASE_SHA" "$RELEASE_SHA"
    return 1
  fi

  printf "Moving tag %s from validated base %s to %s\n" "$TAG_NAME" "$BASE_SHA" "$RELEASE_SHA"
  git tag -f -s -m "$TAG_NAME" -- "$TAG_NAME" "$RELEASE_SHA"
  # Lease the exact tag object, not just its peeled commit. Never overwrite a
  # tag changed by another actor after our preflight check.
  git push --force-with-lease="$TAG_REF:$EXPECTED_TAG_OBJECT" origin "$TAG_REF:$TAG_REF"

  ci_output "release-sha" "$RELEASE_SHA"
}

main "$@"
