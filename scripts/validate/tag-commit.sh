#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Require the release tag to point to the target branch tip before any writes.
# Usage: tag-commit.sh <tag-name> [branch-name] [checkout-sha]
# Outputs: release-base-sha and release-tag-object for the later tag move.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  local TAG_NAME="${1:-}"
  local BRANCH="${2:-main}"
  local CHECKOUT_SHA="${3:-}"
  local TAG_REF="refs/tags/$TAG_NAME"
  local BRANCH_REF="refs/remotes/origin/$BRANCH"

  if [[ -z "$TAG_NAME" ]]; then
    ci_log_error "Usage: tag-commit.sh <tag-name> [branch-name] [checkout-sha]"
    return 1
  fi

  if ! git check-ref-format "$TAG_REF" || ! git check-ref-format "$BRANCH_REF"; then
    ci_log_error "Invalid release tag or branch name"
    return 1
  fi

  printf "## Validating Release Tag Against Branch HEAD\n"

  local TAG_OBJECT TAG_COMMIT BRANCH_HEAD
  if ! TAG_OBJECT=$(git rev-parse --verify "$TAG_REF" 2>/dev/null) ||
    ! TAG_COMMIT=$(git rev-parse --verify "$TAG_REF^{commit}" 2>/dev/null); then
    ci_log_error "Release tag '$TAG_NAME' is missing or does not point to a commit"
    return 1
  fi
  printf "Tag '%s' points to commit: %s\n" "$TAG_NAME" "$TAG_COMMIT"

  if ! BRANCH_HEAD=$(git rev-parse --verify "$BRANCH_REF^{commit}" 2>/dev/null); then
    ci_log_error "Target branch 'origin/$BRANCH' is missing from the checkout"
    return 1
  fi
  printf "Branch '%s' HEAD: %s\n" "$BRANCH" "$BRANCH_HEAD"

  if [[ "$TAG_COMMIT" != "$BRANCH_HEAD" ]]; then
    ci_log_error "Release tag '$TAG_NAME' must point to the HEAD of '$BRANCH' before version bumping"
    printf "Expected: %s\nFound: %s\n" "$BRANCH_HEAD" "$TAG_COMMIT"
    printf "Merge the PR and update your local '%s' before creating a new release tag.\n" "$BRANCH"
    printf "Tags on a pre-merge PR commit or an older branch commit cannot be released by this flow.\n"
    return 1
  fi

  if [[ -n "$CHECKOUT_SHA" && "$CHECKOUT_SHA" != "$BRANCH_HEAD" ]]; then
    ci_log_error "Checkout commit does not match the validated release branch HEAD"
    printf "Expected: %s\nFound: %s\n" "$BRANCH_HEAD" "$CHECKOUT_SHA"
    return 1
  fi

  printf "✓ Tag points to branch HEAD\n"
  ci_output "release-base-sha" "$BRANCH_HEAD"
  ci_output "release-tag-object" "$TAG_OBJECT"
}

main "$@"
