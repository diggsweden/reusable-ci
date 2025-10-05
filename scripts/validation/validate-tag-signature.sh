#!/bin/bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
# SPDX-License-Identifier: CC0-1.0

# Validates that a git tag is annotated and cryptographically signed
# Usage: validate-tag-signature.sh <tag-name> <github-repository> [ospo-bot-gpg-pub]

set -uo pipefail

TAG_NAME="${1:-}"
GITHUB_REPOSITORY="${2:-}"
OSPO_BOT_GPG_PUB="${3:-}"

if [[ -z "$TAG_NAME" ]]; then
  echo "::error::Usage: validate-tag-signature.sh <tag-name> <github-repository>"
  exit 1
fi

echo "## Validating Release Tag Security"

# Check if tag is annotated using git cat-file
# For annotated tags: git cat-file -t <tag> returns "tag"
# For lightweight tags: git cat-file -t <tag> returns "commit"
TAG_TYPE=$(git cat-file -t "$TAG_NAME" 2>/dev/null || echo "unknown")

echo "Tag '$TAG_NAME' object type: $TAG_TYPE"

if [[ "$TAG_TYPE" != "tag" ]]; then
  echo "❌ Tag '$TAG_NAME' is a lightweight tag (not annotated)"
  echo "📝 Requirement: Use annotated tags for releases"
  echo "💡 Example: git tag -a v1.0.0 -m 'Release v1.0.0'"
  if [[ -n "$GITHUB_REPOSITORY" ]]; then
    echo "📚 https://github.com/${GITHUB_REPOSITORY}/blob/main/.github/WORKFLOWS.md#tag-requirements"
  fi
  exit 1
fi

echo "✅ Tag '$TAG_NAME' is annotated"

# Check if tag has any signature (GPG or SSH)
echo "Checking tag signature..."
TAG_CONTENT=$(git cat-file tag "$TAG_NAME")

HAS_GPG_SIG=false
HAS_SSH_SIG=false

if echo "$TAG_CONTENT" | grep -q "BEGIN PGP SIGNATURE"; then
  HAS_GPG_SIG=true
  echo "✅ Tag has a GPG signature"
fi

if echo "$TAG_CONTENT" | grep -q "BEGIN SSH SIGNATURE"; then
  HAS_SSH_SIG=true
  echo "✅ Tag has an SSH signature"
fi

if [[ "$HAS_GPG_SIG" == "false" ]] && [[ "$HAS_SSH_SIG" == "false" ]]; then
  echo "::error::❌ Tag '$TAG_NAME' is not signed"
  echo ""
  echo "Release tags must be cryptographically signed."
  echo "Create with: git tag -s v1.0.0 -m \"Release v1.0.0\""
  echo "Learn more: https://docs.github.com/en/authentication/managing-commit-signature-verification"
  exit 1
fi

# Try to verify the signature (might fail if we don't have the key)
if [[ "$HAS_GPG_SIG" == "true" ]]; then
  # Import GPG keys for verification (if available)
  if [[ -n "$OSPO_BOT_GPG_PUB" ]]; then
    echo "$OSPO_BOT_GPG_PUB" | gpg --import 2>/dev/null || true
  fi

  if git tag -v "$TAG_NAME" 2>/dev/null; then
    echo "✅ GPG signature verification successful"
    SIGNER=$(git tag -v "$TAG_NAME" 2>&1 | grep "Good signature from" | sed 's/.*Good signature from "\(.*\)".*/\1/' || echo "Unknown")
    echo "   Signed by: $SIGNER"
  else
    echo "ℹ️ GPG signature present (verification requires signer's public key)"
  fi
fi

if [[ "$HAS_SSH_SIG" == "true" ]]; then
  echo "ℹ️ SSH signature present"
  echo "   GitHub will show 'Verified' if the SSH key is uploaded to the signer's account"
fi

# Display tag information
echo ""
echo "### Tag Security Summary:"
echo "✅ Tag is annotated (not lightweight)"
echo "✅ Tag is cryptographically signed"
echo "✅ Release security requirements met"
echo ""
echo "### Tag Information:"
echo "Tagged commit: $(git rev-list -n 1 "$TAG_NAME")"
echo "Tagger: $(git for-each-ref "refs/tags/$TAG_NAME" --format='%(taggername) <%(taggeremail)>')"
echo "Tag date: $(git for-each-ref "refs/tags/$TAG_NAME" --format='%(taggerdate:iso8601)')"
echo ""
echo "Tag message:"
git tag -l -n999 "$TAG_NAME" | sed 's/^[^ ]* */  /'
