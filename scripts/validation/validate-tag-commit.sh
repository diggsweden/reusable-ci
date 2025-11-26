#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
# SPDX-License-Identifier: CC0-1.0

# Validates that the commit a tag points to exists in the target branch history
# Usage: validate-tag-commit.sh <tag-name> <branch-name>

set -uo pipefail

TAG_NAME="${1:-}"
BRANCH="${2:-main}"

if [[ -z "$TAG_NAME" ]]; then
  printf "::error::Usage: validate-tag-commit.sh <tag-name> <branch-name>\n"
  exit 1
fi

printf "## Validating Tag Commit is Available on Branch\n"

# Get the commit the tag points to
TAG_COMMIT=$(git rev-parse "$TAG_NAME^{commit}")
printf "Tag '%s' points to commit: %s\n" "$TAG_NAME" "$TAG_COMMIT"

# Get branch HEAD
BRANCH_HEAD=$(git rev-parse "origin/$BRANCH")
printf "Branch '%s' HEAD: %s\n" "$BRANCH" "$BRANCH_HEAD"

# Check if tag commit exists in branch history
if ! git merge-base --is-ancestor "$TAG_COMMIT" "origin/$BRANCH" 2>/dev/null; then
  # Check if tag is ahead of branch
  if git merge-base --is-ancestor "$BRANCH_HEAD" "$TAG_COMMIT" 2>/dev/null; then
    printf "::error::✗ Tag commit is AHEAD of branch HEAD\n"
    printf "\n"
    printf "Tag '%s' points to: %s\n" "$TAG_NAME" "$TAG_COMMIT"
    printf "Branch '%s' is at: %s\n" "$BRANCH" "$BRANCH_HEAD"
    printf "\n"
    printf "This means the commits for this tag were not pushed to '%s' yet.\n" "$BRANCH"
    printf "\n"
    printf "To fix:\n"
    printf "  1. Push your commits first: git push origin %s\n" "$BRANCH"
    printf "  2. Then push the tag: git push origin %s\n" "$TAG_NAME"
    printf "\n"
    printf "⚠️  The workflow will fail because:\n"
    printf "   - Version-bump checks out branch '%s' at %s\n" "$BRANCH" "$BRANCH_HEAD"
    printf "   - Changelog generation won't see tag '%s'\n" "$TAG_NAME"
    printf "   - git-describe will find an older tag instead\n"
    printf "\n"
    exit 1
  else
    printf "::error::✗ Tag commit is not in the history of branch '%s'\n" "$BRANCH"
    printf "\n"
    printf "Tag '%s' points to commit %s\n" "$TAG_NAME" "$TAG_COMMIT"
    printf "This commit is NOT an ancestor of origin/%s\n" "$BRANCH"
    printf "\n"
    printf "This means either:\n"
    printf "  1. The tag is on a different branch\n"
    printf "  2. The tag was created from a stale local branch\n"
    printf "  3. The branches have diverged\n"
    printf "\n"
    printf "To fix:\n"
    printf "  1. Verify: git log --oneline --graph --all\n"
    printf "  2. Ensure tag is on correct branch\n"
    printf "  3. Delete and recreate tag: git tag -d %s && git tag -s %s\n" "$TAG_NAME" "$TAG_NAME"
    printf "\n"
    exit 1
  fi
fi

printf "✓ Tag commit %s is in branch '%s' history\n" "$TAG_COMMIT" "$BRANCH"

# Check position relative to branch HEAD
if [ "$TAG_COMMIT" = "$BRANCH_HEAD" ]; then
  printf "✓ Tag points to branch HEAD (ideal)\n"
else
  printf "ℹ️  Tag commit is an ancestor of branch HEAD\n"
  printf "   This is normal for existing releases\n"
fi
