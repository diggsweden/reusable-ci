#!/bin/bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
# SPDX-License-Identifier: CC0-1.0

# Validates that the commit a tag points to exists in the target branch history
# Usage: validate-tag-commit.sh <tag-name> <branch-name>

set -euo pipefail

TAG_NAME="${1:-}"
BRANCH="${2:-main}"

if [[ -z "$TAG_NAME" ]]; then
  echo "::error::Usage: validate-tag-commit.sh <tag-name> <branch-name>"
  exit 1
fi

echo "## Validating Tag Commit is Available on Branch"

# Get the commit the tag points to
TAG_COMMIT=$(git rev-parse "$TAG_NAME^{commit}")
echo "Tag '$TAG_NAME' points to commit: $TAG_COMMIT"

# Get branch HEAD
BRANCH_HEAD=$(git rev-parse "origin/$BRANCH")
echo "Branch '$BRANCH' HEAD: $BRANCH_HEAD"

# Check if tag commit exists in branch history
if ! git merge-base --is-ancestor "$TAG_COMMIT" "origin/$BRANCH" 2>/dev/null; then
  # Check if tag is ahead of branch
  if git merge-base --is-ancestor "$BRANCH_HEAD" "$TAG_COMMIT" 2>/dev/null; then
    echo "::error::❌ Tag commit is AHEAD of branch HEAD"
    echo ""
    echo "Tag '$TAG_NAME' points to: $TAG_COMMIT"
    echo "Branch '$BRANCH' is at: $BRANCH_HEAD"
    echo ""
    echo "This means the commits for this tag were not pushed to '$BRANCH' yet."
    echo ""
    echo "To fix:"
    echo "  1. Push your commits first: git push origin $BRANCH"
    echo "  2. Then push the tag: git push origin $TAG_NAME"
    echo ""
    echo "⚠️  The workflow will fail because:"
    echo "   - Version-bump checks out branch '$BRANCH' at $BRANCH_HEAD"
    echo "   - Changelog generation won't see tag '$TAG_NAME'"
    echo "   - git-describe will find an older tag instead"
    echo ""
    exit 1
  else
    echo "::error::❌ Tag commit is not in the history of branch '$BRANCH'"
    echo ""
    echo "Tag '$TAG_NAME' points to commit $TAG_COMMIT"
    echo "This commit is NOT an ancestor of origin/$BRANCH"
    echo ""
    echo "This means either:"
    echo "  1. The tag is on a different branch"
    echo "  2. The tag was created from a stale local branch"
    echo "  3. The branches have diverged"
    echo ""
    echo "To fix:"
    echo "  1. Verify: git log --oneline --graph --all"
    echo "  2. Ensure tag is on correct branch"
    echo "  3. Delete and recreate tag: git tag -d $TAG_NAME && git tag -s $TAG_NAME"
    echo ""
    exit 1
  fi
fi

echo "✅ Tag commit $TAG_COMMIT is in branch '$BRANCH' history"

# Check position relative to branch HEAD
if [ "$TAG_COMMIT" = "$BRANCH_HEAD" ]; then
  echo "✅ Tag points to branch HEAD (ideal)"
else
  echo "ℹ️  Tag commit is an ancestor of branch HEAD"
  echo "   This is normal for existing releases"
fi
